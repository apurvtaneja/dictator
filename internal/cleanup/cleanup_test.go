package cleanup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apurvtaneja/dictator/internal/openrouter"
)

type fake struct {
	out string
	err error
	got openrouter.ChatRequest
}

func (f *fake) Chat(_ context.Context, r openrouter.ChatRequest) (openrouter.ChatResult, error) {
	f.got = r
	return openrouter.ChatResult{Text: f.out, Cost: 0.00002}, f.err
}

func TestAppliesGoodEdit(t *testing.T) {
	f := &fake{out: "So I was thinking we should meet at four o'clock."}
	r := Run(context.Background(), f, "m", Options{}, "um so I I was thinking we should meet at three no wait four o'clock")
	if !r.Applied || r.Text != f.out {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(f.got.User, "<dictation>") || f.got.Temperature != 0 {
		t.Fatalf("bad request: %+v", f.got)
	}
}

func TestFallsBackOnError(t *testing.T) {
	r := Run(context.Background(), &fake{err: errors.New("boom")}, "m", Options{}, "hello there")
	if r.Applied || r.Text != "hello there" {
		t.Fatalf("%+v", r)
	}
}

func TestFallsBackWhenModelAnswersInstead(t *testing.T) {
	raw := "what is the capital of france and why is it famous for its museums"
	long := strings.Repeat("Paris is the capital of France and is famous for the Louvre. ", 6)
	r := Run(context.Background(), &fake{out: long}, "m", Options{}, raw)
	if r.Applied || r.Text != raw {
		t.Fatalf("should reject runaway answer: %+v", r)
	}
}

func TestShortFillerHeavyInputMayShrink(t *testing.T) {
	if ok, why := Accept("um uh yeah okay", "Yeah, okay."); !ok {
		t.Fatal(why)
	}
}

func TestNormalizeStripsEchoedTags(t *testing.T) {
	if got := Normalize("<dictation>\nHello.\n</dictation>"); got != "Hello." {
		t.Fatalf("%q", got)
	}
}

func TestExtraInstructionsAppended(t *testing.T) {
	p := Prompt(Options{Extra: "Use British spelling."})
	if !strings.Contains(p, "British spelling") || !strings.HasPrefix(p, systemPrompt) {
		t.Fatal("extra instructions missing")
	}
}

func TestRunAfterSendsContext(t *testing.T) {
	f := &fake{out: "and then we ship it."}
	long := strings.Repeat("x", 400) + " we test it"
	r := RunAfter(context.Background(), f, "m", Options{}, long, "and then we ship it", false)
	if !r.Applied {
		t.Fatalf("%+v", r)
	}
	u := f.got.User
	if !strings.HasPrefix(u, "<before>\n…") || !strings.Contains(u, "we test it\n</before>\n<dictation>\nand then we ship it\n</dictation>") {
		t.Fatalf("bad user message: %q", u)
	}
	if len([]rune(u)) > beforeMax+80 {
		t.Fatalf("context not trimmed: %d runes", len([]rune(u)))
	}
	Run(context.Background(), f, "m", Options{}, "hello there")
	if strings.Contains(f.got.User, "<before>") {
		t.Fatal("Run must not send context")
	}
}

func TestLikelyHallucination(t *testing.T) {
	if !LikelyHallucination(" Thank you. ", 400) || !LikelyHallucination("you", 0) {
		t.Fatal("missed a classic hallucination")
	}
	if LikelyHallucination("Thank you.", 2000) {
		t.Fatal("a real 'thank you' was dropped")
	}
	if LikelyHallucination("Thank you for the update.", 400) {
		t.Fatal("dropped real text")
	}
}

func TestLivePhrasesGetLivePromptAndLoseCutEllipsis(t *testing.T) {
	f := &fake{out: "I was thinking that for the launch we should..."}
	r := RunAfter(context.Background(), f, "m", Options{}, "", "I was thinking that for the launch we should...", false)
	if r.Text != "I was thinking that for the launch we should" {
		t.Fatalf("ellipsis kept: %q", r.Text)
	}
	if !strings.Contains(f.got.System, "cut where the speaker paused") || !strings.Contains(f.got.System, "More may follow") {
		t.Fatal("live prompt missing")
	}
	f.out = "And that's it."
	if r := RunAfter(context.Background(), f, "m", Options{}, "x", "and that's it", true); r.Text != "And that's it." ||
		!strings.Contains(f.got.System, "last phrase") {
		t.Fatalf("last phrase: %q / %q", r.Text, f.got.System)
	}
	Run(context.Background(), f, "m", Options{}, "we should...")
	if strings.Contains(f.got.System, "cut where the speaker paused") {
		t.Fatal("live prompt leaked into a whole dictation")
	}
}

func TestHinglishRuleOnlyWhenOnAndBeforeExtra(t *testing.T) {
	if strings.Contains(Prompt(Options{}), "Hinglish") {
		t.Fatal("Hinglish rule leaked")
	}
	p := Prompt(Options{Hinglish: true, Extra: "Spell my name Apurv."})
	h, e := strings.Index(p, "Hinglish"), strings.Index(p, "Apurv")
	if h < 0 || e < h {
		t.Fatalf("rule missing or after extra: %q", p)
	}
	f := &fake{out: "कल मीटिंग है"}
	if r := Run(context.Background(), f, "m", Options{Hinglish: true}, "कल मीटिंग है"); !r.Applied || r.Reason == "" {
		t.Fatalf("leftover script not flagged: %+v", r)
	}
	f.out = "Kal meeting hai."
	if r := RunAfter(context.Background(), f, "m", Options{Hinglish: true}, "", "कल मीटिंग है", true); r.Reason != "" ||
		!strings.Contains(f.got.System, "Hinglish") {
		t.Fatalf("%+v", r)
	}
}
