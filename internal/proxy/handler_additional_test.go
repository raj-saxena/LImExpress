package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type errTransport struct {
	err error
}

func (t *errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestHandler_UpstreamFailure_Returns502(t *testing.T) {
	original := upstreamClient
	upstreamClient = &http.Client{Transport: &errTransport{err: errors.New("dial failed")}}
	defer func() { upstreamClient = original }()

	h := New(newTestCfg("ak", "ok"))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","max_tokens":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d, want 502", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "upstream request failed") {
		t.Fatalf("expected upstream failure body, got %q", rr.Body.String())
	}
}

func TestHandler_StreamingOpenAI_MalformedChunksIgnored_UsageParsed(t *testing.T) {
	events := []string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"hello"}}]}` + "\n",
		`data: {"usage":` + "\n", // malformed JSON should be ignored
		`data: {"model":"gpt-4o","usage":{"prompt_tokens":11,"completion_tokens":7}}` + "\n",
		"data: [DONE]\n",
		"\n",
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		for _, e := range events {
			_, _ = io.WriteString(w, e)
			f.Flush()
		}
	}))
	defer upstream.Close()

	original := upstreamClient
	upstreamClient = &http.Client{
		Transport: &rewriteTransport{
			base:    http.DefaultTransport,
			rewrite: upstream.URL,
		},
	}
	defer func() { upstreamClient = original }()

	var capturedUsage *Usage
	h := New(newTestCfg("ak", "ok"))
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := &Usage{}
		r = r.WithContext(contextWithUsage(r.Context(), u))
		h.ServeHTTP(w, r)
		capturedUsage = u
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	req = req.WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if capturedUsage == nil {
		t.Fatal("captured usage is nil")
	}
	if capturedUsage.Model != "gpt-4o" {
		t.Fatalf("model=%q, want gpt-4o", capturedUsage.Model)
	}
	if capturedUsage.InputTokens != 11 || capturedUsage.OutputTokens != 7 {
		t.Fatalf("usage=%+v, want prompt=11 completion=7", *capturedUsage)
	}
}
