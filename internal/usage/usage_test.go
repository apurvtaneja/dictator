package usage

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddTodayMonthAndPrune(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.Local)
	var s Stats
	s.Add(now.AddDate(0, 0, -100), 1, 600) // pruned from days, kept in total
	s.Add(now.AddDate(0, 0, -20), 0.5, 60) // last month
	s.Add(now.AddDate(0, 0, -1), 0.2, 60)
	s.Add(now, 0.1, 30)
	if s.Total.Count != 4 || s.Total.Cost != 1.8 {
		t.Fatalf("total %+v", s.Total)
	}
	if len(s.Days) != 3 {
		t.Fatalf("old day not pruned: %v", s.Days)
	}
	if s.Today(now).Cost != 0.1 || s.Month(now).Count != 2 {
		t.Fatalf("today %+v month %+v", s.Today(now), s.Month(now))
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "usage.json")
	if s := Load(); s.Total.Count != 0 || s.Days == nil {
		t.Fatalf("missing file: %+v", s)
	}
	var s Stats
	s.Add(time.Now(), 0.0012, 20)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Total != s.Total {
		t.Fatalf("%+v != %+v", got.Total, s.Total)
	}
}

func TestSummaryAndFormatting(t *testing.T) {
	var s Stats
	if s.MenuLine(time.Now()) != "No spending yet" {
		t.Fatal(s.MenuLine(time.Now()))
	}
	if _, ok := s.PerMinute(); ok {
		t.Fatal("rate from no data")
	}
	s.Add(time.Now(), 0.0033*10, 600) // 10 min at $0.0033/min
	if pm, _ := s.PerMinute(); pm < 0.00329 || pm > 0.00331 {
		t.Fatalf("per minute %v", pm)
	}
	sum := s.Summary()
	if !strings.Contains(sum, "$0.03 on 1 dictation (10 min") || !strings.Contains(sum, "$5 lasts ≈ 25.3 h") {
		t.Fatal(sum)
	}
	if USD(0.00042) != "$0.0004" || USD(1.5) != "$1.50" || USD(0) != "$0" {
		t.Fatal("bad USD formatting")
	}
}
