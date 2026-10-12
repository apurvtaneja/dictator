package openrouter_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/apurvtaneja/dictator/internal/cleanup"
	"github.com/apurvtaneja/dictator/internal/openrouter"
)

// TestLive calls the real OpenRouter API (costs ~$0.0002 per clip). It runs only with
// DICTATOR_LIVE=1, OPENROUTER_API_KEY, and DICTATOR_LIVE_AUDIO=a.m4a[,b.m4a...].
func TestLive(t *testing.T) {
	if os.Getenv("DICTATOR_LIVE") != "1" || os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("set DICTATOR_LIVE=1 and OPENROUTER_API_KEY to run")
	}
	c := openrouter.New(func() string { return os.Getenv("OPENROUTER_API_KEY") }, nil)
	for _, path := range strings.Split(os.Getenv("DICTATOR_LIVE_AUDIO"), ",") {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		t0 := time.Now()
		tr, err := c.Transcribe(ctx, data, path, "openai/whisper-large-v3-turbo", "")
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		sttMs := time.Since(t0).Milliseconds()
		t1 := time.Now()
		r := cleanup.Run(context.Background(), c, "google/gemini-2.5-flash-lite", "", tr.Text)
		t.Logf("\n  file    : %s (%d bytes)\n  raw     : %q  [%d ms, $%.6f]\n  cleaned : %q  [%d ms, $%.6f, applied=%v %s]",
			path, len(data), tr.Text, sttMs, tr.Cost, r.Text, time.Since(t1).Milliseconds(), r.Cost, r.Applied, r.Reason)
	}
}
