// Package app is the controller: hotkey → record → transcribe → clean up →
// insert. All state lives in one goroutine (Loop) fed by channels, so there are
// no locks and nothing polls; between dictations every goroutine is blocked.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/apurvtaneja/dictator/internal/apikey"
	"github.com/apurvtaneja/dictator/internal/audio"
	"github.com/apurvtaneja/dictator/internal/autostart"
	"github.com/apurvtaneja/dictator/internal/cleanup"
	"github.com/apurvtaneja/dictator/internal/config"
	"github.com/apurvtaneja/dictator/internal/hotkey"
	"github.com/apurvtaneja/dictator/internal/mac"
	"github.com/apurvtaneja/dictator/internal/openrouter"
	"github.com/apurvtaneja/dictator/internal/sysproxy"
)

// Options are per-run switches that aren't config keys.
type Options struct {
	NoTray  bool
	Version string
	HUDTest bool // cycle the HUD through its states, then quit
}

type state int

const (
	idle state = iota
	recording
	working
)

type App struct {
	cfg    config.Config // runtime config (includes CLI overrides)
	opts   Options
	client *openrouter.Client

	st       state
	gen      int // identifies the current dictation; stale events are ignored
	rec      *audio.Recorder
	sil      *audio.Silence
	maxTimer *time.Timer
	hk       hotkey.Spec
	hkOK     bool
	lastRaw  string

	// live mode: phrases are typed while the user is still speaking
	phrase *audio.Phrase
	live   *liveRun
	segMu  sync.Mutex
	segQ   []evSegment // closed segments, filled from the recorder's thread

	internal chan any
	Quit     chan struct{} // closed when the app has cleaned up and is about to exit
	quitting bool
}

// internal events
type (
	evAutoStop  struct{ gen int }
	evMaxLength struct{ gen int }
	evPolishing struct{ gen int }
	evResult    struct {
		gen       int
		text, raw string
		cost      float64
		err       error
	}
	evSegment struct {
		gen int
		seg audio.Segment
	}
	evSegReady struct{} // segQ has something
	evSegText  struct {
		gen, idx int
		raw      string
		cost     float64
		err      error
	}
	evSegClean struct {
		gen, idx int
		text     string
		cost     float64
	}
	evMicAnswer struct{ granted bool }
	evKeyCheck  struct {
		text string
		ok   bool
	}
	evShutdown struct{}
)

func New(cfg config.Config, opts Options) *App {
	return &App{
		cfg:      cfg,
		opts:     opts,
		client:   openrouter.New(apikey.Get, sysproxy.ProxyFunc()),
		internal: make(chan any, 64),
		Quit:     make(chan struct{}),
	}
}

// Shutdown asks the app to quit cleanly (safe from any goroutine, e.g. a signal handler).
func (a *App) Shutdown() { a.post(evShutdown{}) }

func (a *App) post(ev any) {
	select {
	case a.internal <- ev:
	default:
		log.Printf("dropped event %T", ev)
	}
}

// Loop is the controller. Run it in its own goroutine; mac.Run owns the main thread.
func (a *App) Loop() {
	for {
		select {
		case ev := <-mac.Events:
			a.onUI(ev)
		case ev := <-a.internal:
			a.onInternal(ev)
		}
		if a.quitting {
			return
		}
	}
}

// ---------------------------------------------------------------- startup / UI

func (a *App) onUI(ev mac.Event) {
	switch ev.Kind {
	case mac.EvReady:
		a.ready()
	case mac.EvHotkey:
		a.onHotkey(ev.Pressed)
	case mac.EvMenu:
		a.onMenu(ev.Tag)
	case mac.EvSettingsSave:
		a.saveSettings(ev.Data)
	case mac.EvSettingsCheckKey:
		go a.checkKey(ev.Data)
	}
}

func (a *App) ready() {
	if !a.opts.NoTray {
		mac.StatusInit()
	}
	autostart.Tidy()
	a.registerHotkey(a.cfg.Hotkey)
	a.refreshMenu()
	if a.opts.HUDTest {
		go a.hudTest()
		return
	}
	ax := mac.AXTrusted()
	log.Printf("ready — hotkey %s, stt %s, cleanup %v (%s), insert %s, accessibility %v",
		a.hk.Normalised, a.cfg.STTModel, a.cfg.Cleanup, a.cfg.CleanupModel, a.cfg.Insert, ax)
	if !ax && a.cfg.Insert != "clipboard" {
		mac.AXPrompt() // typing needs it; macOS shows its own dialog
	}
	if apikey.Get() == "" || os.Getenv("DICTATOR_SETTINGS_SELFTEST") != "" {
		a.status("Add an OpenRouter key to start")
		a.openSettings(true)
	}
}

func (a *App) registerHotkey(spec string) error {
	hk, err := hotkey.Parse(spec)
	if err == nil {
		if st := mac.RegisterHotkey(hk.KeyCode, hk.Modifiers); st != 0 {
			err = fmt.Errorf("%s is taken by another app (OSStatus %d)", hk.Normalised, st)
		}
	}
	if err != nil {
		a.hkOK = false
		a.status("Hotkey error: " + err.Error())
		log.Printf("hotkey: %v", err)
		return err
	}
	a.hk, a.hkOK = hk, true
	a.idleStatus()
	return nil
}

func (a *App) idleStatus() {
	switch {
	case apikey.Get() == "":
		a.status("Add an OpenRouter key to start")
	case !a.hkOK:
		// keep the hotkey error visible
	default:
		a.status("Ready — " + prettyHotkey(a.hk.Normalised))
	}
}

func (a *App) status(s string) {
	if !a.opts.NoTray {
		mac.ItemTitle(mac.ItemStatus, s)
	}
}

func (a *App) refreshMenu() {
	if a.opts.NoTray {
		return
	}
	mac.ItemHidden(mac.ItemAddKey, apikey.Get() != "")
	mac.ItemChecked(mac.ItemModeToggle, a.cfg.Mode != "ptt")
	mac.ItemChecked(mac.ItemModePTT, a.cfg.Mode == "ptt")
	mac.ItemChecked(mac.ItemCleanup, a.cfg.Cleanup)
	mac.ItemChecked(mac.ItemLogin, autostart.Enabled())
	mac.ItemEnabled(mac.ItemCopyRaw, a.lastRaw != "")
}

func (a *App) onMenu(tag int) {
	switch tag {
	case mac.ItemAddKey, mac.ItemSettings:
		a.openSettings(tag == mac.ItemAddKey)
	case mac.ItemModeToggle, mac.ItemModePTT:
		a.cfg.Mode = map[bool]string{true: "ptt", false: "toggle"}[tag == mac.ItemModePTT]
		a.persist("mode")
	case mac.ItemCleanup:
		a.cfg.Cleanup = !a.cfg.Cleanup
		a.persist("cleanup")
	case mac.ItemLogin:
		if err := autostart.Set(!autostart.Enabled()); err != nil {
			a.hud(mac.HUDError, err.Error(), 3600)
		} else if autostart.NeedsApproval() {
			a.hud(mac.HUDError, "Allow Dictator in System Settings ▸ Login Items", 4000)
		}
	case mac.ItemCopyRaw:
		if a.lastRaw != "" {
			mac.SetClipboard(a.lastRaw)
			a.hud(mac.HUDSuccess, "Raw transcript copied", 1800)
		}
	case mac.ItemLog:
		mac.OpenURL("file://" + config.LogPath)
	case mac.ItemAbout:
		mac.About(a.opts.Version)
	case mac.ItemQuit:
		a.shutdown()
	}
	a.refreshMenu()
}

func (a *App) persist(keys ...string) {
	if err := config.Update(a.cfg, keys...); err != nil {
		log.Printf("saving config: %v", err)
	}
}

// ---------------------------------------------------------------- dictation

func (a *App) onHotkey(pressed bool) {
	ptt := a.cfg.Mode == "ptt"
	switch {
	case pressed && ptt:
		a.begin()
	case !pressed && ptt:
		a.end()
	case pressed && a.st == recording:
		a.end()
	case pressed && a.st == idle:
		a.begin()
	}
}

func (a *App) begin() {
	if a.st != idle {
		return
	}
	if apikey.Get() == "" {
		a.hud(mac.HUDError, "Add your OpenRouter key in Settings", 3600)
		a.openSettings(true)
		return
	}
	switch audio.MicStatus() {
	case audio.PermUnknown:
		a.hud(mac.HUDWorking, "Allow microphone access…", 0)
		go func() { a.post(evMicAnswer{granted: audio.RequestMic()}) }()
		return
	case audio.PermDenied, audio.PermRestricted:
		a.hud(mac.HUDError, "Microphone blocked — enable Dictator in Privacy & Security", 5000)
		mac.OpenURL("x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone")
		return
	}

	a.gen++
	gen := a.gen
	path := filepath.Join(os.TempDir(), fmt.Sprintf("dictator-%d-%d.m4a", os.Getpid(), gen))
	autoStop := a.cfg.SilenceAutoStop && a.cfg.Mode != "ptt"
	sil := &audio.Silence{
		Threshold: a.cfg.SilenceThreshold,
		SilenceMs: a.cfg.SilenceMs,
		MinMs:     a.cfg.SilenceMinMs,
		NothingMs: 8000,
	}
	var fired atomic.Bool
	var recRef atomic.Pointer[audio.Recorder]
	var phrase *audio.Phrase
	var onSegment func(audio.Segment)
	if a.cfg.LiveTyping() {
		phrase = &audio.Phrase{
			Threshold:   a.cfg.SilenceThreshold,
			PauseMs:     a.cfg.LivePauseMs,
			MinSpeechMs: 1500, // shorter clips transcribe noticeably worse
			MaxMs:       15000,
			HardMs:      5000,
		}
		onSegment = func(s audio.Segment) { // recorder thread: queue it, never block
			a.segMu.Lock()
			a.segQ = append(a.segQ, evSegment{gen, s})
			a.segMu.Unlock()
			a.post(evSegReady{})
		}
	}
	rec, err := audio.Start(path, func(rms float64, ms int) {
		// audio thread: only cheap arithmetic, and at most one event per recording
		if sil.Feed(rms, ms) && autoStop && fired.CompareAndSwap(false, true) {
			a.post(evAutoStop{gen})
		}
		if phrase != nil {
			if cut, speech := phrase.Feed(rms, ms); cut {
				if r := recRef.Load(); r != nil {
					r.Cut(speech)
				}
			}
		}
	}, onSegment)
	if err != nil {
		log.Printf("record: %v", err)
		a.hud(mac.HUDError, "Couldn't open the microphone", 3600)
		return
	}
	recRef.Store(rec)
	a.rec, a.sil, a.phrase, a.st = rec, sil, phrase, recording
	if phrase != nil {
		a.live = &liveRun{gen: gen, pid: mac.FrontmostPID(), last: -1, raws: map[int]string{}, rawAll: map[int]string{}}
	}
	a.maxTimer = time.AfterFunc(time.Duration(a.cfg.MaxSeconds)*time.Second, func() { a.post(evMaxLength{gen}) })
	go a.client.Warm() // TLS handshake overlaps with speaking
	a.cue("Tink")
	a.icon(mac.IconRecording)
	a.hud(mac.HUDRecording, "Recording…", 0)
	a.status("Recording…")
}

func (a *App) end() {
	if a.st != recording {
		return
	}
	a.maxTimer.Stop()
	secs, last := a.rec.Stop() // synchronous: the level callback has finished after this
	if a.phrase != nil {
		last.SpeechMs += a.phrase.SpeechMs()
	}
	rec, sil := a.rec, a.sil
	a.rec, a.sil, a.phrase = nil, nil, nil
	a.cue("Pop")

	if a.live != nil && sil.Spoke() && secs >= 0.3 {
		a.st = working
		a.icon(mac.IconBusy)
		a.hud(mac.HUDWorking, "Finishing…", 0)
		a.status("Finishing…")
		a.drainSegments()
		a.startSegment(last)
		return
	}
	if secs < 0.3 || !sil.Spoke() {
		os.Remove(rec.Path)
		a.live = nil
		a.st = idle
		a.icon(mac.IconIdle)
		a.hud(mac.HUDError, "No speech detected", 2200)
		a.idleStatus()
		return // nothing sent, nothing billed
	}
	a.st = working
	a.icon(mac.IconBusy)
	a.hud(mac.HUDWorking, "Transcribing…", 0)
	a.status("Transcribing…")
	go a.process(a.gen, rec.Path, a.cfg)
}

// process runs off the controller goroutine: network + cleanup.
func (a *App) process(gen int, path string, cfg config.Config) {
	defer debug.FreeOSMemory() // hand the transient buffers back to the OS
	defer os.Remove(path)
	start := time.Now()
	data, err := os.ReadFile(path)
	if err != nil {
		a.post(evResult{gen: gen, err: err})
		return
	}
	tr, err := a.transcribe(path, data, cfg)
	if err != nil {
		a.post(evResult{gen: gen, err: err})
		return
	}
	sttMs := time.Since(start).Milliseconds()
	res := evResult{gen: gen, text: tr.Text, raw: tr.Text, cost: tr.Cost}
	if cfg.Cleanup && tr.Text != "" {
		a.post(evPolishing{gen})
		c := cleanup.Run(context.Background(), a.client, cfg.CleanupModel, cleanup.FromConfig(cfg), tr.Text)
		res.text, res.cost = c.Text, res.cost+c.Cost
		if !c.Applied {
			log.Printf("cleanup skipped: %s", c.Reason)
		}
	}
	log.Printf("dictation: %d bytes audio, stt %d ms, total %d ms, cost $%.6f",
		len(data), sttMs, time.Since(start).Milliseconds(), res.cost)
	a.post(res)
}

// transcribe sends one audio file, retrying as WAV if the model rejects m4a.
func (a *App) transcribe(path string, data []byte, cfg config.Config) (openrouter.Transcription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tr, err := a.client.Transcribe(ctx, data, path, cfg.STTModel, cfg.STTLanguage())
	if openrouter.KindOf(err) == openrouter.KindBadRequest && strings.HasSuffix(path, ".m4a") {
		// some models only take WAV: convert natively and retry once
		wav := strings.TrimSuffix(path, ".m4a") + ".wav"
		if cerr := audio.ToWAV(path, wav); cerr == nil {
			defer os.Remove(wav)
			if wdata, rerr := os.ReadFile(wav); rerr == nil {
				log.Printf("m4a rejected (%v); retrying as WAV", err)
				tr, err = a.client.Transcribe(ctx, wdata, wav, cfg.STTModel, cfg.STTLanguage())
			}
		}
	}
	return tr, err
}

func (a *App) onInternal(ev any) {
	switch e := ev.(type) {
	case evAutoStop:
		if e.gen == a.gen {
			a.end()
		}
	case evMaxLength:
		if e.gen == a.gen && a.st == recording {
			a.end()
		}
	case evPolishing:
		if e.gen == a.gen && a.st == working {
			a.hud(mac.HUDPolishing, "Polishing…", 0)
			a.status("Polishing…")
		}
	case evResult:
		if e.gen == a.gen {
			a.finish(e)
		}
	case evMicAnswer:
		if e.granted {
			a.hud(mac.HUDSuccess, "Microphone allowed — press "+prettyHotkey(a.hk.Normalised)+" again", 3000)
		} else {
			a.hud(mac.HUDError, "Microphone access was denied", 3600)
		}
	case evSegReady:
		a.drainSegments()
	case evSegText:
		a.onSegText(e)
	case evSegClean:
		a.onSegClean(e)
	case evKeyCheck:
		mac.SettingsStatus("key", e.text, e.ok)
	case evShutdown:
		a.shutdown()
	}
}

func (a *App) finish(r evResult) {
	a.st = idle
	a.icon(mac.IconIdle)
	if r.err != nil {
		msg := friendly(r.err)
		log.Printf("dictation failed: %v", r.err)
		a.cue("Basso")
		a.hud(mac.HUDError, msg, 4000)
		a.status(msg)
		return
	}
	text := strings.TrimSpace(r.text)
	if text == "" {
		a.hud(mac.HUDError, "No speech detected", 2200)
		a.idleStatus()
		return
	}
	a.lastRaw = r.raw
	if !a.opts.NoTray {
		mac.ItemEnabled(mac.ItemCopyRaw, true)
	}
	if a.cfg.TrailingSpace {
		text += " "
	}
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	ax := mac.AXTrusted()
	if a.cfg.Insert != "clipboard" && ax {
		how := "typed"
		if a.cfg.Insert == "paste" || len(text) > a.cfg.PasteThreshold {
			how = "pasted"
			paste(text)
		} else {
			mac.TypeText(text, 18, 4)
		}
		log.Printf("insert: mode %s, %d chars %s", a.cfg.Insert, n, how)
		a.hud(mac.HUDSuccess, fmt.Sprintf("Inserted %d characters", n), 1600)
		a.idleStatus()
		return
	}
	mac.SetClipboard(text)
	if a.cfg.Insert == "clipboard" {
		log.Printf("insert: mode clipboard, %d chars copied", n)
		a.hud(mac.HUDSuccess, "Copied — press ⌘V", 3200)
	} else {
		log.Printf("insert: mode %s but Accessibility not granted, %d chars copied", a.cfg.Insert, n)
		a.hud(mac.HUDError, "Copied — allow Dictator in Accessibility to type", 4000)
	}
	a.status(fmt.Sprintf("Copied %d characters — press ⌘V", n))
}

// ---------------------------------------------------------------- live mode
//
// Each phrase (a segment cut at a pause) is transcribed as soon as it closes,
// several at once if need be. Cleanup and typing then run strictly in order,
// one phrase at a time, so every phrase is cleaned knowing what came before it.

type liveRun struct {
	gen      int
	pid      int            // app that was in front when recording started
	last     int            // index of the final segment, -1 until recording stops
	next     int            // next segment to clean and insert
	raws     map[int]string // transcribed, waiting for their turn ("" = nothing to insert)
	rawAll   map[int]string // for "Copy raw transcript"
	cleaning bool
	before   string // tail of what has been inserted, as cleanup context
	inserted int    // characters typed or pasted
	held     []string
	divert   string // why text is going to the clipboard instead ("" = typing)
	cost     float64
	lastErr  error
	started  time.Time // when recording stopped
}

func (a *App) drainSegments() {
	a.segMu.Lock()
	q := a.segQ
	a.segQ = nil
	a.segMu.Unlock()
	for _, e := range q {
		if a.live != nil && e.gen == a.live.gen {
			a.startSegment(e.seg)
		} else {
			os.Remove(e.seg.Path)
		}
	}
}

func (a *App) startSegment(seg audio.Segment) {
	l := a.live
	if seg.Final {
		l.last = seg.Index
		l.started = time.Now()
	}
	if seg.SpeechMs < 300 { // a breath or a click: don't send (or pay for) it
		os.Remove(seg.Path)
		l.raws[seg.Index] = ""
		a.advance()
		return
	}
	cfg := a.cfg
	go func() {
		defer os.Remove(seg.Path)
		t0 := time.Now()
		data, err := os.ReadFile(seg.Path)
		var tr openrouter.Transcription
		if err == nil {
			tr, err = a.transcribe(seg.Path, data, cfg)
		}
		if err == nil && cleanup.LikelyHallucination(tr.Text, seg.SpeechMs) {
			log.Printf("phrase %d: dropped %q (%d ms of speech)", seg.Index, tr.Text, seg.SpeechMs)
			tr.Text = ""
		}
		log.Printf("phrase %d: %d bytes, %d ms speech, stt %d ms", seg.Index, len(data), seg.SpeechMs, time.Since(t0).Milliseconds())
		a.post(evSegText{gen: l.gen, idx: seg.Index, raw: tr.Text, cost: tr.Cost, err: err})
	}()
}

func (a *App) onSegText(e evSegText) {
	l := a.live
	if l == nil || e.gen != l.gen {
		return
	}
	l.cost += e.cost
	if e.err != nil {
		log.Printf("phrase %d failed: %v", e.idx, e.err)
		l.lastErr = e.err // reported when the dictation ends, not over the recording HUD
	}
	l.raws[e.idx] = strings.TrimSpace(e.raw)
	l.rawAll[e.idx] = l.raws[e.idx]
	a.advance()
}

// advance cleans and inserts the phrases that are next in line, then finishes
// the dictation once the final one is in.
func (a *App) advance() {
	l := a.live
	for !l.cleaning {
		raw, ok := l.raws[l.next]
		if !ok {
			break
		}
		if raw == "" {
			delete(l.raws, l.next)
			l.next++
			continue
		}
		if !a.cfg.Cleanup {
			a.insertPhrase(raw)
			delete(l.raws, l.next)
			l.next++
			continue
		}
		l.cleaning = true
		idx, before, cfg, last := l.next, l.before, a.cfg, l.next == l.last
		if a.st == working {
			a.hud(mac.HUDPolishing, "Polishing…", 0)
		}
		go func() {
			c := cleanup.RunAfter(context.Background(), a.client, cfg.CleanupModel, cleanup.FromConfig(cfg), before, raw, last)
			if !c.Applied {
				log.Printf("phrase %d: cleanup skipped: %s", idx, c.Reason)
			}
			a.post(evSegClean{gen: l.gen, idx: idx, text: c.Text, cost: c.Cost})
		}()
		return
	}
	if l.last >= 0 && l.next > l.last && !l.cleaning {
		a.finishLive()
	}
}

func (a *App) onSegClean(e evSegClean) {
	l := a.live
	if l == nil || e.gen != l.gen {
		return
	}
	l.cleaning = false
	l.cost += e.cost
	a.insertPhrase(e.text)
	delete(l.raws, e.idx)
	l.next = e.idx + 1
	a.advance()
}

// insertPhrase types one phrase where the cursor is -- unless typing isn't
// possible or the user has switched apps, in which case it (and everything
// after it) is held for the clipboard.
func (a *App) insertPhrase(text string) {
	l := a.live
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// cleanup may open with punctuation that closes the previous phrase (". What…")
	if l.before != "" && !strings.HasSuffix(l.before, "\n") && !strings.ContainsAny(text[:1], ".,;:!?") {
		text = " " + text
	}
	if l.divert == "" {
		switch {
		case a.cfg.Insert == "clipboard":
			l.divert = "clipboard"
		case !mac.AXTrusted():
			l.divert = "accessibility"
		case l.pid != 0 && mac.FrontmostPID() != l.pid:
			l.divert = "switched"
		}
		if l.divert != "" {
			log.Printf("live: holding text for the clipboard (%s)", l.divert)
		}
	}
	if l.divert != "" {
		l.held = append(l.held, text)
	} else {
		a.insertNow(text)
	}
	l.inserted += utf8.RuneCountInString(strings.TrimSpace(text))
	l.before += text
	if len(l.before) > 1200 {
		l.before = l.before[len(l.before)-600:]
		for !utf8.RuneStart(l.before[0]) {
			l.before = l.before[1:]
		}
	}
}

func (a *App) insertNow(text string) {
	if a.cfg.Insert == "paste" || len(text) > a.cfg.PasteThreshold {
		paste(text)
	} else {
		mac.TypeText(text, 18, 4)
	}
}

func (a *App) finishLive() {
	l := a.live
	a.live = nil
	a.st = idle
	a.icon(mac.IconIdle)
	var parts []string
	idx := make([]int, 0, len(l.rawAll))
	for i := range l.rawAll {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		if l.rawAll[i] != "" {
			parts = append(parts, l.rawAll[i])
		}
	}
	log.Printf("live dictation: %d phrases, %d chars, %d ms after stop, cost $%.6f",
		len(idx), l.inserted, time.Since(l.started).Milliseconds(), l.cost)
	debug.FreeOSMemory()

	if l.inserted == 0 {
		if l.lastErr != nil {
			msg := friendly(l.lastErr)
			a.cue("Basso")
			a.hud(mac.HUDError, msg, 4000)
			a.status(msg)
		} else {
			a.hud(mac.HUDError, "No speech detected", 2200)
			a.idleStatus()
		}
		return
	}
	a.lastRaw = strings.Join(parts, " ")
	if !a.opts.NoTray {
		mac.ItemEnabled(mac.ItemCopyRaw, true)
	}
	if l.divert == "" {
		if a.cfg.TrailingSpace {
			mac.TypeText(" ", 1, 0)
		}
		log.Printf("insert: live, %d chars typed", l.inserted)
		if l.lastErr != nil {
			a.hud(mac.HUDError, "Part of it was lost — "+friendly(l.lastErr), 4000)
		} else {
			a.hud(mac.HUDSuccess, fmt.Sprintf("Inserted %d characters", l.inserted), 1600)
		}
		a.idleStatus()
		return
	}
	held := strings.TrimSpace(strings.Join(l.held, ""))
	if a.cfg.TrailingSpace {
		held += " "
	}
	mac.SetClipboard(held)
	log.Printf("insert: live, %d chars held for the clipboard (%s)", utf8.RuneCountInString(held), l.divert)
	switch l.divert {
	case "switched":
		a.hud(mac.HUDError, "You switched apps — the rest is copied, press ⌘V", 4000)
	case "accessibility":
		a.hud(mac.HUDError, "Copied — allow Dictator in Accessibility to type", 4000)
	default:
		a.hud(mac.HUDSuccess, "Copied — press ⌘V", 3200)
	}
	a.status("Copied — press ⌘V")
}

// paste puts text on the clipboard, presses ⌘V, then restores what was there.
func paste(text string) {
	prev := mac.Clipboard()
	mac.SetClipboard(text)
	time.Sleep(40 * time.Millisecond) // let the pasteboard settle
	mac.PressCmdV()
	if prev != "" {
		time.AfterFunc(400*time.Millisecond, func() { mac.SetClipboard(prev) })
	}
}

func friendly(err error) string {
	switch openrouter.KindOf(err) {
	case openrouter.KindBadKey:
		return "OpenRouter key rejected — check Settings"
	case openrouter.KindNoCredits:
		return "OpenRouter credits used up — top up at openrouter.ai"
	case openrouter.KindRateLimited:
		return "Rate limited by OpenRouter — try again shortly"
	case openrouter.KindTimeout:
		return "Transcription timed out"
	case openrouter.KindBadRequest:
		return "Request rejected — check the speech model in Settings"
	case openrouter.KindNetwork:
		return "Network error — are you online?"
	}
	return "Transcription failed"
}

func (a *App) hud(kind int, text string, autohideMs int) {
	if !a.cfg.ShowHUD && kind != mac.HUDHide {
		return
	}
	mac.HUD(kind, text, autohideMs, corner(a.cfg.HUDCorner))
}

func (a *App) icon(s int) {
	if !a.opts.NoTray {
		mac.StatusIcon(s)
	}
}

func (a *App) cue(name string) {
	if a.cfg.SoundCues {
		mac.Sound(name)
	}
}

func corner(c string) int {
	switch c {
	case "bottom-right":
		return 1
	case "top-right":
		return 2
	}
	return 0
}

func prettyHotkey(h string) string {
	r := strings.NewReplacer("option", "⌥", "alt", "⌥", "opt", "⌥", "cmd", "⌘", "command", "⌘",
		"ctrl", "⌃", "control", "⌃", "shift", "⇧", "+", "")
	return strings.ToUpper(r.Replace(h))
}

func (a *App) shutdown() {
	if a.rec != nil {
		a.rec.Stop()
		os.Remove(a.rec.Path)
		a.rec = nil
	}
	mac.UnregisterHotkey()
	a.quitting = true
	close(a.Quit)
}

// ---------------------------------------------------------------- settings

// Presets and the note shown under each picker (speeds measured October 2026).
var sttPresets = []string{
	"openai/gpt-4o-mini-transcribe",
	"mistralai/voxtral-mini-transcribe",
	"elevenlabs/scribe-v2",
	"openai/whisper-large-v3-turbo",
}

var sttNotes = map[string]string{
	"openai/gpt-4o-mini-transcribe":     "Fast (~1.1 s) and accurate, with punctuation. About $0.003 per minute of audio.",
	"mistralai/voxtral-mini-transcribe": "Fast (~1 s) and nearly as accurate. About $0.003 per minute.",
	"elevenlabs/scribe-v2":              "Very accurate, a little slower (~1.4 s). About $0.002 per minute.",
	"openai/whisper-large-v3-turbo":     "Cheapest (≈ $0.01 per hour), but mishears more technical words.",
}

var cleanupPresets = []string{
	"deepseek/deepseek-v4.1-flash",
	"anthropic/claude-haiku-5.5",
	"google/gemini-2.5-flash-lite",
}

var cleanupNotes = map[string]string{
	"deepseek/deepseek-v4.1-flash": "Fastest (~0.7 s) and keeps your wording. OpenRouter may route text to DeepSeek's own servers.",
	"anthropic/claude-haiku-5.5":   "Careful (~1.2 s) and lightly polishes tone. Runs on Anthropic's servers in the US.",
	"google/gemini-2.5-flash-lite": "Cheapest, but misses some self-corrections (\"Tuesday, no, Wednesday\").",
}

func (a *App) openSettings(focusKey bool) {
	keyHint := "No key yet — create one at openrouter.ai/keys (pay as you go)."
	switch apikey.Source() {
	case "file":
		keyHint = "Saved: " + apikey.Masked(apikey.Get()) + " (in ~/.config/dictator, only you can read it)"
	case "env":
		keyHint = "Using OPENROUTER_API_KEY from the environment."
	}
	in := map[string]any{
		"api_key":              "",
		"has_key":              apikey.Get() != "",
		"key_hint":             keyHint,
		"focus_key":            focusKey || apikey.Get() == "",
		"stt_model":            a.cfg.STTModel,
		"stt_presets":          sttPresets,
		"stt_model_notes":      sttNotes,
		"language":             a.cfg.Language,
		"cleanup":              a.cfg.Cleanup,
		"cleanup_model":        a.cfg.CleanupModel,
		"cleanup_presets":      cleanupPresets,
		"cleanup_model_notes":  cleanupNotes,
		"cleanup_instructions": a.cfg.CleanupInstructions,
		"hotkey":               a.cfg.Hotkey,
		"hotkey_keys":          hotkey.Names(),
		"mode":                 a.cfg.Mode,
		"start_at_login":       autostart.Enabled(),
		"login_supported":      true,
		"insert":               a.cfg.Insert,
		"typing":               map[bool]string{true: "live", false: "finish"}[a.cfg.Live],
		"trailing_space":       a.cfg.TrailingSpace,
		"accessibility":        mac.AXTrusted(),
		"silence_auto_stop":    a.cfg.SilenceAutoStop,
		"silence_ms":           a.cfg.SilenceMs,
		"silence_threshold":    a.cfg.SilenceThreshold,
		"max_seconds":          a.cfg.MaxSeconds,
		"show_hud":             a.cfg.ShowHUD,
		"hud_corner":           a.cfg.HUDCorner,
		"sound_cues":           a.cfg.SoundCues,
	}
	data, _ := json.Marshal(in)
	mac.SettingsOpen(string(data))
}

// settingsForm is what the Settings window sends back (numbers arrive as text).
type settingsForm struct {
	APIKey              string   `json:"api_key"`
	STTModel            string   `json:"stt_model"`
	Language            string   `json:"language"`
	Cleanup             flexBool `json:"cleanup"`
	CleanupModel        string   `json:"cleanup_model"`
	CleanupInstructions string   `json:"cleanup_instructions"`
	Hotkey              string   `json:"hotkey"`
	Mode                string   `json:"mode"`
	StartAtLogin        flexBool `json:"start_at_login"`
	Insert              string   `json:"insert"`
	Typing              string   `json:"typing"`
	TrailingSpace       flexBool `json:"trailing_space"`
	SilenceAutoStop     flexBool `json:"silence_auto_stop"`
	SilenceMs           string   `json:"silence_ms"`
	SilenceThreshold    string   `json:"silence_threshold"`
	MaxSeconds          string   `json:"max_seconds"`
	ShowHUD             flexBool `json:"show_hud"`
	HUDCorner           string   `json:"hud_corner"`
	SoundCues           flexBool `json:"sound_cues"`
}

// flexBool accepts JSON true/false and also 1/0, so a sender that encodes
// booleans as numbers can never block saving.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch strings.TrimSpace(string(data)) {
	case "true", "1":
		*b = true
	case "false", "0", "null":
		*b = false
	default:
		return fmt.Errorf("not a boolean: %s", data)
	}
	return nil
}

func (a *App) saveSettings(data string) {
	var f settingsForm
	if err := json.Unmarshal([]byte(data), &f); err != nil {
		log.Printf("settings form rejected: %v", err)
		mac.SettingsStatus("save", "Couldn't read the form: "+err.Error(), false)
		return
	}
	next := a.cfg
	next.STTModel, next.Language = f.STTModel, strings.ToLower(f.Language)
	next.Cleanup, next.CleanupModel, next.CleanupInstructions = bool(f.Cleanup), f.CleanupModel, f.CleanupInstructions
	next.Hotkey, next.Mode, next.Insert, next.TrailingSpace = f.Hotkey, f.Mode, f.Insert, bool(f.TrailingSpace)
	next.Live = f.Typing != "finish"
	next.SilenceAutoStop, next.ShowHUD, next.HUDCorner, next.SoundCues = bool(f.SilenceAutoStop), bool(f.ShowHUD), f.HUDCorner, bool(f.SoundCues)

	var err error
	if next.SilenceMs, err = intIn(f.SilenceMs, 300, 10000, "Pause length"); err == nil {
		if next.MaxSeconds, err = intIn(f.MaxSeconds, 5, 600, "Max length"); err == nil {
			next.SilenceThreshold, err = floatIn(f.SilenceThreshold, 0.001, 0.5, "Silence level")
		}
	}
	if err == nil && next.STTModel == "" {
		err = errors.New("Choose a speech model")
	}
	if err == nil && next.Cleanup && next.CleanupModel == "" {
		err = errors.New("Choose a cleanup model (or turn cleanup off)")
	}
	if err == nil && next.Language == config.Hinglish && !next.Cleanup {
		err = errors.New("Hinglish needs grammar cleanup on")
	}
	if err == nil {
		_, err = hotkey.Parse(next.Hotkey)
	}
	if err != nil {
		log.Printf("settings invalid: %v", err)
		mac.SettingsStatus("save", err.Error(), false)
		return
	}

	if next.Hotkey != a.cfg.Hotkey {
		if herr := a.registerHotkey(next.Hotkey); herr != nil {
			_ = a.registerHotkey(a.cfg.Hotkey) // roll back
			mac.SettingsStatus("save", herr.Error(), false)
			return
		}
	}
	if f.APIKey != "" {
		if kerr := apikey.Set(f.APIKey); kerr != nil {
			mac.SettingsStatus("save", kerr.Error(), false)
			return
		}
	}
	if bool(f.StartAtLogin) != autostart.Enabled() {
		if lerr := autostart.Set(bool(f.StartAtLogin)); lerr != nil {
			log.Printf("login item: %v", lerr)
		}
	}

	// persist only what changed: the running cfg may carry CLI overrides
	changed := diffKeys(a.cfg, next)
	a.cfg = next
	if len(changed) > 0 {
		a.persist(changed...)
	}
	log.Printf("settings saved (%s)", strings.Join(changed, ", "))
	mac.SettingsClose()
	a.refreshMenu()
	a.idleStatus()
}

func diffKeys(old, cur config.Config) []string {
	var a, b map[string]any
	da, _ := json.Marshal(old)
	db, _ := json.Marshal(cur)
	json.Unmarshal(da, &a)
	json.Unmarshal(db, &b)
	var keys []string
	for k, v := range b {
		if fmt.Sprint(a[k]) != fmt.Sprint(v) {
			keys = append(keys, k)
		}
	}
	return keys
}

func intIn(s string, lo, hi int, name string) (int, error) {
	var v int
	if _, err := fmt.Sscan(strings.TrimSpace(s), &v); err != nil || v < lo || v > hi {
		return 0, fmt.Errorf("%s must be a whole number from %d to %d", name, lo, hi)
	}
	return v, nil
}

func floatIn(s string, lo, hi float64, name string) (float64, error) {
	var v float64
	if _, err := fmt.Sscan(strings.TrimSpace(s), &v); err != nil || v < lo || v > hi {
		return 0, fmt.Errorf("%s must be a number from %g to %g", name, lo, hi)
	}
	return v, nil
}

// checkKey runs off the controller goroutine.
func (a *App) checkKey(typed string) {
	key := strings.TrimSpace(typed)
	if key == "" {
		key = apikey.Get()
	}
	if key == "" {
		a.post(evKeyCheck{"Paste a key first", false})
		return
	}
	c := openrouter.New(func() string { return key }, sysproxy.ProxyFunc())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := c.KeyInfo(ctx)
	if err != nil {
		a.post(evKeyCheck{friendly(err), false})
		return
	}
	msg := fmt.Sprintf("✓ Valid — $%.4f used", info.Usage)
	if info.LimitRemaining != nil {
		msg += fmt.Sprintf(", $%.2f left on this key", *info.LimitRemaining)
	}
	a.post(evKeyCheck{msg, true})
}

// ---------------------------------------------------------------- diagnostics

func (a *App) hudTest() {
	steps := []struct {
		kind int
		text string
		wait time.Duration
	}{
		{mac.HUDRecording, "Recording…", 2 * time.Second},
		{mac.HUDWorking, "Transcribing…", 1500 * time.Millisecond},
		{mac.HUDPolishing, "Polishing…", 1500 * time.Millisecond},
		{mac.HUDSuccess, "Copied — press ⌘V", 2500 * time.Millisecond},
		{mac.HUDError, "No speech detected", 2500 * time.Millisecond},
	}
	for _, s := range steps {
		log.Printf("HUD: %s", s.text)
		mac.HUD(s.kind, s.text, 0, corner(a.cfg.HUDCorner))
		time.Sleep(s.wait)
	}
	mac.HUD(mac.HUDHide, "", 0, 0)
	time.Sleep(400 * time.Millisecond)
	a.Shutdown()
}
