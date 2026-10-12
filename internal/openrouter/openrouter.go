// Package openrouter is a minimal OpenRouter client: speech-to-text, chat (for
// the grammar cleanup) and key info. One shared HTTP/2 connection is reused
// across calls and dropped after 90 s idle, so nothing lingers between
// dictations.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultBase = "https://openrouter.ai/api/v1"
	referer     = "https://github.com/apurvtaneja/dictator"
	title       = "Dictator"
)

// Error kinds, shown to the user as friendly messages.
const (
	KindBadKey      = "bad-key"
	KindNoCredits   = "no-credits"
	KindRateLimited = "rate-limited"
	KindBadRequest  = "bad-request"
	KindServer      = "server"
	KindNetwork     = "network"
	KindTimeout     = "timeout"
)

type Error struct {
	Kind   string
	Status int
	Msg    string
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("openrouter %s (HTTP %d): %s", e.Kind, e.Status, e.Msg)
	}
	return fmt.Sprintf("openrouter %s: %s", e.Kind, e.Msg)
}

// KindOf returns the error kind, or "" for nil.
func KindOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	if err != nil {
		return KindNetwork
	}
	return ""
}

type Client struct {
	Base string
	Key  func() string
	http *http.Client
}

func New(key func() string, proxy func(*http.Request) (*url.URL, error)) *Client {
	tr := &http.Transport{
		Proxy:               proxy,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        2,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	}
	base := DefaultBase
	// OPENROUTER_BASE_URL points at an OpenRouter-compatible gateway (e.g. a
	// company's API proxy) or a local mock for testing.
	if b := strings.TrimRight(os.Getenv("OPENROUTER_BASE_URL"), "/"); b != "" {
		base = b
	}
	return &Client{Base: base, Key: key, http: &http.Client{Transport: tr}}
}

// Warm opens (or keeps alive) the TLS connection while the user is still
// speaking, so the real request doesn't pay for the handshake. Best effort.
func (c *Client) Warm() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.Base+"/models", nil)
	if err != nil {
		return
	}
	if resp, err := c.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Key())
	req.Header.Set("HTTP-Referer", referer)
	req.Header.Set("X-Title", title)
	return req, nil
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return &Error{Kind: KindTimeout, Msg: err.Error()}
		}
		return &Error{Kind: KindNetwork, Msg: err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return &Error{Kind: KindNetwork, Msg: err.Error()}
	}
	if resp.StatusCode/100 != 2 {
		return statusError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &Error{Kind: KindServer, Status: resp.StatusCode, Msg: "bad JSON: " + err.Error()}
	}
	return nil
}

func statusError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		msg = e.Error.Message
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	kind := KindServer
	switch {
	case status == 401 || status == 403:
		kind = KindBadKey
	case status == 402:
		kind = KindNoCredits
	case status == 429:
		kind = KindRateLimited
	case status == 408 || status == 504:
		kind = KindTimeout
	case status >= 400 && status < 500:
		kind = KindBadRequest
	}
	return &Error{Kind: kind, Status: status, Msg: msg}
}

// Transcription is the result of speech-to-text.
type Transcription struct {
	Text string
	Cost float64
}

// Transcribe sends an audio file (m4a, wav, ...; the name's extension tells
// the server the format) to /audio/transcriptions.
func (c *Client) Transcribe(ctx context.Context, audio []byte, filename, model, language string) (Transcription, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", model)
	if language != "" {
		_ = mw.WriteField("language", language)
	}
	_ = mw.WriteField("response_format", "json")
	fw, err := mw.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return Transcription{}, err
	}
	fw.Write(audio)
	mw.Close()

	req, err := c.newRequest(ctx, http.MethodPost, "/audio/transcriptions", &buf)
	if err != nil {
		return Transcription{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	var out struct {
		Text  string `json:"text"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := c.do(req, &out); err != nil {
		return Transcription{}, err
	}
	return Transcription{Text: strings.TrimSpace(out.Text), Cost: out.Usage.Cost}, nil
}

// ChatRequest is the small subset of /chat/completions this app needs.
type ChatRequest struct {
	Model       string
	System      string
	User        string
	MaxTokens   int
	Temperature float64
}

type ChatResult struct {
	Text string
	Cost float64
}

func (c *Client) Chat(ctx context.Context, r ChatRequest) (ChatResult, error) {
	body := map[string]any{
		"model": r.Model,
		"messages": []map[string]string{
			{"role": "system", "content": r.System},
			{"role": "user", "content": r.User},
		},
		"temperature": r.Temperature,
		"max_tokens":  r.MaxTokens,
		"stream":      false,
		// cleanup is an edit, not a puzzle: thinking only adds latency
		"reasoning": map[string]any{"enabled": false},
		"provider":  map[string]any{"sort": "latency"},
		"usage":     map[string]any{"include": true},
	}
	data, _ := json.Marshal(body)
	req, err := c.newRequest(ctx, http.MethodPost, "/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := c.do(req, &out); err != nil {
		return ChatResult{}, err
	}
	if len(out.Choices) == 0 {
		return ChatResult{}, &Error{Kind: KindServer, Msg: "no choices in response"}
	}
	return ChatResult{Text: out.Choices[0].Message.Content, Cost: out.Usage.Cost}, nil
}

// KeyInfo is what GET /key reports about the API key.
type KeyInfo struct {
	Label          string   `json:"label"`
	Usage          float64  `json:"usage"`
	Limit          *float64 `json:"limit"`
	LimitRemaining *float64 `json:"limit_remaining"`
	IsFreeTier     bool     `json:"is_free_tier"`
}

func (c *Client) KeyInfo(ctx context.Context) (KeyInfo, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "/key", nil)
	if err != nil {
		return KeyInfo{}, err
	}
	var out struct {
		Data KeyInfo `json:"data"`
	}
	if err := c.do(req, &out); err != nil {
		return KeyInfo{}, err
	}
	return out.Data, nil
}
