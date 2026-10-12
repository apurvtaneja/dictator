// Dictator: press a hotkey anywhere, speak, and clean text lands where your
// cursor is. Speech-to-text and grammar cleanup run on OpenRouter with the
// user's own API key.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/apurvtaneja/dictator/internal/apikey"
	"github.com/apurvtaneja/dictator/internal/app"
	"github.com/apurvtaneja/dictator/internal/audio"
	"github.com/apurvtaneja/dictator/internal/autostart"
	"github.com/apurvtaneja/dictator/internal/cleanup"
	"github.com/apurvtaneja/dictator/internal/config"
	"github.com/apurvtaneja/dictator/internal/daemon"
	"github.com/apurvtaneja/dictator/internal/hotkey"
	"github.com/apurvtaneja/dictator/internal/mac"
	"github.com/apurvtaneja/dictator/internal/openrouter"
	"github.com/apurvtaneja/dictator/internal/sysproxy"
)

// version is set at build time: -ldflags "-X main.version=1.2.3"
var version = "1.0.0-dev"

// AppKit must own the process's main thread.
func init() { runtime.LockOSThread() }

type flags struct {
	detach, stop, status, restart              bool
	noTray, noHUD, noSound, clipboard, minimal bool
	cleanup, noCleanup                         bool
	insert, mode, hotkey, sttModel, cleanupMdl string
	configPath, autostartArg                   string
	check, micTest, hudTest, showVersion       bool
	setKey                                     bool
}

func parseFlags(args []string) (*flags, error) {
	f := &flags{}
	fs := flag.NewFlagSet("dictator", flag.ContinueOnError)
	fs.Usage = func() { usage(fs.Output()) }
	b := func(p *bool, names ...string) {
		for _, n := range names {
			fs.BoolVar(p, n, false, "")
		}
	}
	s := func(p *string, name string) { fs.StringVar(p, name, "", "") }
	b(&f.detach, "d", "detach")
	b(&f.stop, "stop")
	b(&f.status, "status")
	b(&f.restart, "restart")
	b(&f.noTray, "no-tray")
	b(&f.noHUD, "no-hud")
	b(&f.noSound, "no-sound")
	b(&f.clipboard, "clipboard")
	b(&f.minimal, "minimal")
	b(&f.cleanup, "cleanup")
	b(&f.noCleanup, "no-cleanup")
	s(&f.insert, "insert")
	s(&f.mode, "mode")
	s(&f.hotkey, "hotkey")
	s(&f.sttModel, "stt-model")
	s(&f.cleanupMdl, "cleanup-model")
	s(&f.configPath, "config")
	s(&f.autostartArg, "autostart")
	b(&f.check, "check")
	b(&f.micTest, "mic-test")
	b(&f.hudTest, "hud-test")
	b(&f.showVersion, "version", "v")
	b(&f.setKey, "set-key")
	// Finder may pass -psn_0_12345 on old macOS versions; drop it.
	var clean []string
	for _, a := range args {
		if !strings.HasPrefix(a, "-psn_") {
			clean = append(clean, a)
		}
	}
	return f, fs.Parse(clean)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `Dictator — press a hotkey anywhere, speak, and clean text lands where your cursor is.

Usage: dictator [flags]

Running in the background:
  -d, --detach        start in the background and give the terminal back
  --stop              stop the running instance
  --status            is it running? (pid, log path)
  --restart           stop, then start detached

Features (this run only; config.json is not changed):
  --no-tray           no menu-bar icon (quit with --stop)
  --no-hud            no on-screen indicator
  --no-sound          no start/stop sounds
  --clipboard         only copy the text; you press ⌘V
  --minimal           all of the above: --no-tray --no-hud --no-sound --clipboard
  --insert MODE       auto | clipboard | keystroke | paste
  --mode MODE         toggle | ptt
  --hotkey KEY        e.g. option+d, ctrl+shift+d, f6
  --stt-model ID      OpenRouter speech-to-text model
  --cleanup / --no-cleanup        grammar cleanup on/off
  --cleanup-model ID  OpenRouter model used for the cleanup
  --config PATH       use another config file

Setup and diagnostics:
  --set-key           save the OpenRouter API key (reads stdin)
  --autostart on|off  start at login
  --check             print diagnostics
  --mic-test          record until you pause, then print raw + cleaned text
  --hud-test          show each on-screen indicator state
  --version
`)
}

func main() {
	f, err := parseFlags(os.Args[1:])
	if err == flag.ErrHelp {
		return
	}
	if err != nil {
		os.Exit(2)
	}
	if f.configPath != "" {
		config.UsePath(f.configPath)
	}
	os.Exit(run(f))
}

func run(f *flags) int {
	switch {
	case f.showVersion:
		fmt.Println("Dictator", version)
		return 0
	case f.status:
		if pid := daemon.RunningPID(); pid != 0 {
			fmt.Printf("Dictator is running (pid %d).\n  log: %s\n", pid, config.LogPath)
			return 0
		}
		fmt.Println("Dictator is not running.")
		return 1
	case f.stop:
		return doStop()
	case f.restart:
		doStop()
		return doDetach()
	case f.detach:
		return doDetach()
	case f.setKey:
		return doSetKey()
	case f.autostartArg != "":
		if err := autostart.Set(f.autostartArg == "on"); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("start at login:", map[bool]string{true: "on", false: "off"}[autostart.Enabled()])
		return 0
	}

	if err := config.EnsureExists(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		log.Print(err)
	}
	applyOverrides(&cfg, f)

	switch {
	case f.check:
		return doCheck(cfg)
	case f.micTest:
		return doMicTest(cfg)
	}
	return runApp(cfg, f)
}

func applyOverrides(cfg *config.Config, f *flags) {
	if f.minimal {
		f.noTray, f.noHUD, f.noSound, f.clipboard = true, true, true, true
	}
	if f.noHUD {
		cfg.ShowHUD = false
	}
	if f.noSound {
		cfg.SoundCues = false
	}
	if f.clipboard {
		cfg.Insert = "clipboard"
	}
	if f.insert != "" {
		cfg.Insert = f.insert
	}
	if f.mode != "" {
		cfg.Mode = f.mode
	}
	if f.hotkey != "" {
		cfg.Hotkey = f.hotkey
	}
	if f.sttModel != "" {
		cfg.STTModel = f.sttModel
	}
	if f.cleanupMdl != "" {
		cfg.CleanupModel = f.cleanupMdl
	}
	if f.cleanup {
		cfg.Cleanup = true
	}
	if f.noCleanup {
		cfg.Cleanup = false
	}
}

// childArgs is argv for the detached copy: the same switches minus process control.
func childArgs() []string {
	var out []string
	for _, a := range os.Args[1:] {
		switch strings.TrimLeft(a, "-") {
		case "d", "detach", "restart":
			continue
		}
		out = append(out, a)
	}
	return out
}

func doDetach() int {
	if pid := daemon.RunningPID(); pid != 0 {
		fmt.Printf("Dictator is already running (pid %d). Use --restart to replace it.\n", pid)
		return 1
	}
	pid, err := daemon.Detach(childArgs())
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not start:", err)
		return 1
	}
	fmt.Printf("Dictator started in the background (pid %d).\n  log:  %s\n  stop: %s --stop\n",
		pid, config.LogPath, filepath.Base(os.Args[0]))
	return 0
}

func doStop() int {
	pid, err := daemon.Stop(5 * time.Second)
	switch {
	case pid == 0:
		fmt.Println("Dictator is not running.")
	case err != nil:
		fmt.Printf("Could not stop pid %d: %v\n", pid, err)
		return 1
	default:
		fmt.Printf("Stopped Dictator (pid %d).\n", pid)
	}
	return 0
}

func doSetKey() int {
	if isTerminal(os.Stdin) {
		fmt.Print("Paste your OpenRouter API key (from openrouter.ai/keys) and press Return: ")
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if err := apikey.Set(strings.TrimSpace(line)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if strings.TrimSpace(line) == "" {
		fmt.Println("Key removed.")
	} else {
		fmt.Println("Saved", apikey.Masked(strings.TrimSpace(line)), "to", apikey.Path)
	}
	return 0
}

func doCheck(cfg config.Config) int {
	fmt.Printf("Dictator %s diagnostics\n", version)
	exe, _ := os.Executable()
	fmt.Println("  executable       :", exe, map[bool]string{true: "(app bundle)", false: ""}[mac.InBundle()])
	fmt.Println("  config file      :", config.Path)
	fmt.Println("  log file         :", config.LogPath)
	if pid := daemon.RunningPID(); pid != 0 {
		fmt.Printf("  running          : yes (pid %d)\n", pid)
	} else {
		fmt.Println("  running          : no")
	}
	if k := apikey.Get(); k != "" {
		fmt.Printf("  API key          : %s (from %s)\n", apikey.Masked(k), apikey.Source())
	} else {
		fmt.Println("  API key          : none — add one in Settings or with --set-key")
	}
	if hk, err := hotkey.Parse(cfg.Hotkey); err == nil {
		fmt.Println("  hotkey / mode    :", hk.Normalised, "/", cfg.Mode)
	} else {
		fmt.Println("  hotkey           : INVALID —", err)
	}
	fmt.Println("  speech model     :", cfg.STTModel, langNote(cfg.Language))
	fmt.Printf("  cleanup          : %v (%s)\n", cfg.Cleanup, cfg.CleanupModel)
	fmt.Println("  insert           :", cfg.Insert, map[bool]string{true: "(Accessibility granted: typing works)",
		false: "(no Accessibility: text is copied, you press ⌘V)"}[mac.AXTrusted()])
	mic := map[int]string{audio.PermUnknown: "not asked yet", audio.PermRestricted: "restricted by policy",
		audio.PermDenied: "DENIED", audio.PermGranted: "granted"}[audio.MicStatus()]
	fmt.Println("  microphone       :", mic)
	fmt.Println("  start at login   :", autostart.Enabled())
	sp := sysproxy.Read()
	proxy := "direct"
	if p := os.Getenv("HTTPS_PROXY"); p != "" {
		proxy = p + " (HTTPS_PROXY)"
	} else if sp.HTTPS != "" {
		proxy = sp.HTTPS + " (system)"
	}
	if sp.PAC {
		proxy += "; a PAC file is configured but not evaluated — set HTTPS_PROXY if requests fail"
	}
	fmt.Println("  network proxy    :", proxy)
	if apikey.Get() != "" {
		c := openrouter.New(apikey.Get, sysproxy.ProxyFunc())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		start := time.Now()
		if info, err := c.KeyInfo(ctx); err != nil {
			fmt.Println("  openrouter.ai    : ERROR —", err)
		} else {
			fmt.Printf("  openrouter.ai    : reachable in %d ms; key valid, $%.4f used\n",
				time.Since(start).Milliseconds(), info.Usage)
		}
	}
	return 0
}

func langNote(l string) string {
	if l == "" {
		return "(language: auto)"
	}
	return "(language: " + l + ")"
}

// doMicTest records until a pause (max 15 s), then prints raw and cleaned text.
func doMicTest(cfg config.Config) int {
	if apikey.Get() == "" {
		fmt.Println("No API key. Run: dictator --set-key")
		return 1
	}
	if audio.MicStatus() == audio.PermUnknown && !audio.RequestMic() {
		fmt.Println("Microphone access denied.")
		return 1
	}
	path := filepath.Join(os.TempDir(), fmt.Sprintf("dictator-mictest-%d.m4a", os.Getpid()))
	defer os.Remove(path)
	sil := &audio.Silence{Threshold: cfg.SilenceThreshold, SilenceMs: cfg.SilenceMs, MinMs: cfg.SilenceMinMs, NothingMs: 8000}
	done := make(chan struct{}, 1)
	rec, err := audio.Start(path, func(rms float64, ms int) {
		if sil.Feed(rms, ms) {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}, nil)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	c := openrouter.New(apikey.Get, sysproxy.ProxyFunc())
	go c.Warm()
	fmt.Println("Recording — speak now; it stops after a pause (max 15 s)…")
	select {
	case <-done:
	case <-time.After(15 * time.Second):
	}
	secs, _ := rec.Stop()
	st, _ := os.Stat(path)
	fmt.Printf("Captured %.1f s, peak level %.3f, %s file %d bytes\n", secs, sil.MaxRMS,
		map[bool]string{true: "AAC", false: "WAV"}[rec.AAC], st.Size())
	if !sil.Spoke() {
		fmt.Println("No speech detected (nothing sent).")
		return 1
	}
	data, _ := os.ReadFile(path)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t0 := time.Now()
	tr, err := c.Transcribe(ctx, data, path, cfg.STTModel, cfg.STTLanguage())
	if err != nil {
		fmt.Println("transcription failed:", err)
		return 1
	}
	fmt.Printf("\nRaw     (%s, %d ms, $%.6f):\n  %s\n", cfg.STTModel, time.Since(t0).Milliseconds(), tr.Cost, tr.Text)
	if cfg.Cleanup {
		t1 := time.Now()
		r := cleanup.Run(context.Background(), c, cfg.CleanupModel, cleanup.FromConfig(cfg), tr.Text)
		note := ""
		if !r.Applied {
			note = " — fell back to raw: " + r.Reason
		}
		fmt.Printf("Cleaned (%s, %d ms, $%.6f%s):\n  %s\n", cfg.CleanupModel, time.Since(t1).Milliseconds(), r.Cost, note, r.Text)
	}
	return 0
}

func runApp(cfg config.Config, f *flags) int {
	interactive := isTerminal(os.Stdout)
	if !interactive {
		// Finder/launchd start us with no terminal: write to the log file.
		daemon.RotateLog()
		if lf, err := os.OpenFile(config.LogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			log.SetOutput(lf)
		}
	}
	lock, ok, err := daemon.Lock()
	if err != nil {
		log.Print(err)
		return 1
	}
	if !ok {
		msg := "Dictator is already running (dictator --stop, or menu ▸ Quit)."
		fmt.Println(msg)
		log.Print(msg)
		return 1
	}
	defer lock.Close()
	_ = daemon.WritePID()

	a := app.New(cfg, app.Options{NoTray: f.noTray, Version: version, HUDTest: f.hudTest})
	go a.Loop()

	// SIGTERM (--stop, logout) and Ctrl+C quit cleanly. os/signal is handled by
	// the Go runtime without any periodic wakeups.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		<-sigs
		a.Shutdown()
	}()
	go func() {
		<-a.Quit
		daemon.ClearPID()
		log.Print("bye")
		mac.Quit() // AppKit exits the process
	}()

	if interactive {
		fmt.Printf("Dictator %s is running — hotkey %s. Ctrl+C quits (or start with -d to free this terminal).\n",
			version, cfg.Hotkey)
	}
	log.Printf("Dictator %s started (pid %d)", version, os.Getpid())
	mac.Run() // never returns
	return 0
}

// isTerminal is a real isatty: /dev/null (what Finder gives us) is a character
// device too, so a mode check isn't enough.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}
