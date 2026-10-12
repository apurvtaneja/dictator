package audio

import "testing"

func feed(s *Silence, rms float64, ms, n int) (stoppedAt int) {
	for i := 0; i < n; i++ {
		if s.Feed(rms, ms) {
			return i
		}
	}
	return -1
}

func TestStopsAfterPauseFollowingSpeech(t *testing.T) {
	s := &Silence{Threshold: 0.02, SilenceMs: 1800, MinMs: 700, NothingMs: 8000}
	if feed(s, 0.1, 100, 10) != -1 { // 1 s of speech
		t.Fatal("stopped during speech")
	}
	if at := feed(s, 0.001, 100, 30); at != 17 { // 18 quiet chunks = 1.8 s
		t.Fatalf("stopped at chunk %d, want 17", at)
	}
}

func TestWaitsForSpeechBeforeAutoStop(t *testing.T) {
	s := &Silence{Threshold: 0.02, SilenceMs: 1800, MinMs: 700, NothingMs: 8000}
	if at := feed(s, 0.001, 100, 79); at != -1 {
		t.Fatalf("cut off a slow start at chunk %d", at)
	}
	if !s.Feed(0.001, 100) { // 8 s of nothing
		t.Fatal("should give up after NothingMs")
	}
	if s.Spoke() {
		t.Fatal("never spoke")
	}
}

func TestPhraseCutsAtPauseAfterEnoughSpeech(t *testing.T) {
	p := &Phrase{Threshold: 0.02, PauseMs: 700, MinSpeechMs: 1500, MaxMs: 15000, HardMs: 5000}
	step := func(rms float64, n int) (int, int) {
		for i := 0; i < n; i++ {
			if cut, sp := p.Feed(rms, 100); cut {
				return i, sp
			}
		}
		return -1, 0
	}
	// 1 s of speech then a pause: too little speech to cut
	if at, _ := step(0.1, 10); at != -1 {
		t.Fatal("cut during speech")
	}
	if at, _ := step(0.001, 10); at != -1 {
		t.Fatalf("cut a 1 s phrase at %d", at)
	}
	// more speech, then a 0.7 s pause cuts, crediting 2 s of speech
	step(0.1, 10)
	if at, sp := step(0.001, 10); at != 6 || sp != 2000 {
		t.Fatalf("cut at %d with %d ms speech, want 6 and 2000", at, sp)
	}
	if p.SpeechMs() != 0 {
		t.Fatal("counters not reset after cut")
	}
}

func TestPhraseCutsLongRunOn(t *testing.T) {
	p := &Phrase{Threshold: 0.02, PauseMs: 700, MinSpeechMs: 1500, MaxMs: 3000, HardMs: 1000}
	n := 0
	for ; n < 100; n++ {
		if cut, _ := p.Feed(0.1, 100); cut { // never a pause
			break
		}
	}
	if n != 39 { // MaxMs + HardMs = 4 s
		t.Fatalf("hard cut at chunk %d, want 39", n)
	}
	for n = 0; n < 100; n++ {
		rms := 0.1
		if n == 32 {
			rms = 0.001 // one quiet buffer after MaxMs
		}
		if cut, _ := p.Feed(rms, 100); cut {
			break
		}
	}
	if n != 32 {
		t.Fatalf("soft cut at chunk %d, want 32", n)
	}
}
