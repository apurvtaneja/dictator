package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTest(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(func() string { return "sk-test" }, nil)
	c.Base = srv.URL
	return c
}

func TestTranscribeSendsMultipart(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Errorf("bad request %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(f)
		if hdr.Filename != "a.m4a" || string(data) != "AUDIO" || r.FormValue("model") != "m1" || r.FormValue("language") != "en" {
			t.Errorf("form: %q %q %q %q", hdr.Filename, data, r.FormValue("model"), r.FormValue("language"))
		}
		w.Write([]byte(`{"text":" hello world ","usage":{"cost":0.00004}}`))
	})
	tr, err := c.Transcribe(context.Background(), []byte("AUDIO"), "/tmp/a.m4a", "m1", "en")
	if err != nil || tr.Text != "hello world" || tr.Cost != 0.00004 {
		t.Fatalf("%+v %v", tr, err)
	}
}

func TestErrorKinds(t *testing.T) {
	for status, kind := range map[int]string{401: KindBadKey, 402: KindNoCredits, 429: KindRateLimited, 400: KindBadRequest, 502: KindServer} {
		c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			w.Write([]byte(`{"error":{"message":"nope"}}`))
		})
		_, err := c.Transcribe(context.Background(), []byte("x"), "a.wav", "m", "")
		if KindOf(err) != kind {
			t.Errorf("status %d: kind %q, want %q (%v)", status, KindOf(err), kind, err)
		}
	}
}

func TestChatDisablesReasoningAndParsesContent(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["reasoning"].(map[string]any)["enabled"] != false || body["temperature"].(float64) != 0 {
			t.Errorf("body: %v", body)
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"Fixed."}}],"usage":{"cost":0.00001}}`))
	})
	res, err := c.Chat(context.Background(), ChatRequest{Model: "m", System: "s", User: "u", MaxTokens: 10})
	if err != nil || res.Text != "Fixed." {
		t.Fatalf("%+v %v", res, err)
	}
}
