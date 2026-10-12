package audio

// Silence decides when a recording should stop on its own.
//
// It stops after SilenceMs of continuous quiet, but only once the speaker has
// said something (so a slow start isn't cut off), and never inside the first
// MinMs. If nothing is said at all for NothingMs, it stops too.
type Silence struct {
	Threshold float64 // RMS (0..1) below which audio counts as quiet
	SilenceMs int
	MinMs     int
	NothingMs int

	elapsed int
	quiet   int
	spoke   bool
	MaxRMS  float64
}

// Feed takes one level reading covering ms milliseconds and reports whether
// the recording should stop now.
func (s *Silence) Feed(rms float64, ms int) bool {
	s.elapsed += ms
	if rms > s.MaxRMS {
		s.MaxRMS = rms
	}
	if rms >= s.Threshold {
		s.spoke = true
		s.quiet = 0
		return false
	}
	s.quiet += ms
	if !s.spoke {
		return s.NothingMs > 0 && s.elapsed >= s.NothingMs
	}
	return s.elapsed >= s.MinMs && s.quiet >= s.SilenceMs
}

// Spoke reports whether any reading reached the speech threshold.
func (s *Silence) Spoke() bool { return s.spoke }

// Phrase decides where live mode cuts a recording: at a short pause once the
// phrase has enough speech to transcribe well, or at the first quiet moment
// after MaxMs so a long run-on still shows progress (and at MaxMs+HardMs no
// matter what).
type Phrase struct {
	Threshold   float64
	PauseMs     int // quiet that ends a phrase
	MinSpeechMs int // speech a phrase needs before it may be cut
	MaxMs       int
	HardMs      int

	segMs, speechMs, quiet int
}

// Feed takes one level reading and reports whether to cut now, with the
// speech the finished phrase contained. Counting restarts after a cut.
func (p *Phrase) Feed(rms float64, ms int) (cut bool, speechMs int) {
	p.segMs += ms
	loud := rms >= p.Threshold
	if loud {
		p.speechMs += ms
		p.quiet = 0
	} else {
		p.quiet += ms
	}
	switch {
	case p.speechMs >= p.MinSpeechMs && p.quiet >= p.PauseMs,
		p.speechMs > 0 && p.segMs >= p.MaxMs && !loud,
		p.speechMs > 0 && p.segMs >= p.MaxMs+p.HardMs:
		speechMs = p.speechMs
		p.segMs, p.speechMs, p.quiet = 0, 0, 0
		return true, speechMs
	}
	return false, 0
}

// SpeechMs is the speech in the phrase still being recorded.
func (p *Phrase) SpeechMs() int { return p.speechMs }
