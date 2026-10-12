// Package usage keeps a local tally of what dictations cost, from the exact
// cost OpenRouter reports with each request. It counts this Mac only: other
// apps using the same key are not included.
package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/apurvtaneja/dictator/internal/config"
)

// Path is the tally file (a var so tests can point it elsewhere).
var Path = filepath.Join(config.Dir, "usage.json")

// keepDays bounds the per-day history: enough for "this month".
const keepDays = 62

type Tally struct {
	Cost    float64 `json:"cost"`    // US dollars
	Seconds float64 `json:"seconds"` // audio recorded
	Count   int     `json:"count"`   // dictations
}

func (t *Tally) add(o Tally) {
	t.Cost += o.Cost
	t.Seconds += o.Seconds
	t.Count += o.Count
}

type Stats struct {
	Total Tally            `json:"total"`
	Days  map[string]Tally `json:"days"` // "2006-01-02", local time
}

const dayFmt = "2006-01-02"

// Load reads the tally; a missing or unreadable file is an empty one.
func Load() Stats {
	var s Stats
	if data, err := os.ReadFile(Path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	if s.Days == nil {
		s.Days = map[string]Tally{}
	}
	return s
}

// Add counts one dictation and drops days older than keepDays.
func (s *Stats) Add(now time.Time, cost, secs float64) {
	if s.Days == nil {
		s.Days = map[string]Tally{}
	}
	t := Tally{Cost: cost, Seconds: secs, Count: 1}
	s.Total.add(t)
	day := now.Format(dayFmt)
	d := s.Days[day]
	d.add(t)
	s.Days[day] = d
	cutoff := now.AddDate(0, 0, -keepDays).Format(dayFmt)
	for k := range s.Days {
		if k < cutoff {
			delete(s.Days, k)
		}
	}
}

func (s Stats) Today(now time.Time) Tally { return s.Days[now.Format(dayFmt)] }

func (s Stats) Month(now time.Time) Tally {
	var t Tally
	prefix := now.Format("2006-01-")
	for k, d := range s.Days {
		if len(k) == len(dayFmt) && k[:len(prefix)] == prefix {
			t.add(d)
		}
	}
	return t
}

// Save writes the tally atomically.
func (s Stats) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path), 0o700); err != nil {
		return err
	}
	tmp := Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path)
}

// PerMinute is the average cost per minute of audio, once there is at least
// a minute to average over.
func (s Stats) PerMinute() (float64, bool) {
	if s.Total.Seconds < 60 || s.Total.Cost <= 0 {
		return 0, false
	}
	return s.Total.Cost / (s.Total.Seconds / 60), true
}

// MenuLine is the short summary shown in the menu bar menu.
func (s Stats) MenuLine(now time.Time) string {
	if s.Total.Count == 0 {
		return "No spending yet"
	}
	return "Spent " + USD(s.Today(now).Cost) + " today · " + USD(s.Month(now).Cost) + " this month"
}

// Summary is the longer explanation shown in Settings.
func (s Stats) Summary() string {
	if s.Total.Count == 0 {
		return "Spending: nothing yet. Dictator adds up the cost of each dictation on this Mac."
	}
	out := fmt.Sprintf("Spent so far: %s on %d dictation%s (%s of speech).",
		USD(s.Total.Cost), s.Total.Count, map[bool]string{true: "", false: "s"}[s.Total.Count == 1], duration(s.Total.Seconds))
	if pm, ok := s.PerMinute(); ok {
		out += fmt.Sprintf(" About %s per minute, so $5 lasts ≈ %s.", USD(pm), duration(5/pm*60))
	}
	return out + " Counted on this Mac only."
}

// USD formats small amounts with enough digits to be meaningful.
func USD(v float64) string {
	switch {
	case v <= 0:
		return "$0"
	case v < 0.01:
		return fmt.Sprintf("$%.4f", v)
	default:
		return fmt.Sprintf("$%.2f", v)
	}
}

func duration(secs float64) string {
	switch {
	case secs < 60:
		return fmt.Sprintf("%.0f s", secs)
	case secs < 3600:
		return fmt.Sprintf("%.0f min", secs/60)
	default:
		return fmt.Sprintf("%.1f h", secs/3600)
	}
}
