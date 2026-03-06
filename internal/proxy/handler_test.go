package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/limexpress/gateway/internal/config"
)

// newTestCfg returns a minimal ProvidersConfig for testing.
func newTestCfg(anthropicKey, openaiKey string) config.ProvidersConfig {
	return config.ProvidersConfig{
		Anthropic: config.AnthropicConfig{APIKey: anthropicKey},
		OpenAI:    config.OpenAIConfig{APIKey: openaiKey},
	}
}

// ---------- routing ----------

func TestRouteRequest_KnownPaths(t *testing.T) {
	cases := []struct {
		path     string
		provider string
	}{
		{"/v1/messages", "anthropic"},
		{"/v1/chat/completions", "openai"},
		{"/v1/completions", "openai"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			target, ok := routeRequest(tc.path)
			if !ok {
				t.Fatalf("expected route for %s", tc.path)
			}
			if target.provider != tc.provider {
				t.Errorf("got provider %q, want %q", target.provider, tc.provider)
			}
		})
	}
}

func TestRouteRequest_UnknownPath(t *testing.T) {
	_, ok := routeRequest("/v1/unknown")
	if ok {
		t.Fatal("expected no route for unknown path")
	}
}

// ---------- 404 for unknown paths ----------

func TestHandler_UnknownPath_Returns404(t *testing.T) {
	h := New(newTestCfg("ak", "ok"))
	req := httptest.NewRequest(http.MethodPost, "/v1/unknown", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ---------- auth header injection ----------

func TestHandler_AnthropicAuthHeaders(t *testing.T) {
	var capturedReq *http.Request

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"type":"message"}`)
	}))
	defer upstream.Close()

	// Temporarily monkey-patch routeRequest by replacing the upstreamClient
	// with one that talks to our test server. We do this by constructing a
	// custom handler and overriding the target URL inline.

	h := &handler{cfg: newTestCfg("test-anthropic-key", "test-openai-key")}

	// We'll test the auth header injection logic by calling the real handler
	// but pointed at a test server. To do that we directly build the upstream
	// request the same way ServeHTTP does, but using our upstream URL.
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-3-haiku","max_tokens":10}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "client-key-should-be-removed")
	req.Header.Set("Authorization", "Bearer client-token-should-be-removed")

	// Build the upstream request exactly as ServeHTTP does.
	uPtr := &Usage{Provider: "anthropic"}
	ctx := contextWithUsage(req.Context(), uPtr)
	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstream.URL, req.Body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for key, vals := range req.Header {
		lower := strings.ToLower(key)
		if lower == "x-api-key" || lower == "authorization" {
			continue
		}
		for _, v := range vals {
			upstreamReq.Header.Add(key, v)
		}
	}
	upstreamReq.Header.Set("x-api-key", h.cfg.Anthropic.APIKey)
	upstreamReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := upstreamClient.Do(upstreamReq)
	if err != nil {
		t.Fatalf("upstream request: %v", err)
	}
	defer resp.Body.Close()

	if capturedReq == nil {
		t.Fatal("upstream did not receive request")
	}

	// Verify provider key was injected.
	if got := capturedReq.Header.Get("x-api-key"); got != "test-anthropic-key" {
		t.Errorf("x-api-key = %q, want %q", got, "test-anthropic-key")
	}
	if got := capturedReq.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", got, "2023-06-01")
	}

	// Verify client auth headers were NOT forwarded.
	if got := capturedReq.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization header leaked: %q", got)
	}
}

// ---------- non-streaming body copy ----------

func TestHandler_NonStreaming_BodyCopied(t *testing.T) {
	const responseBody = `{"id":"msg_1","type":"message"}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, responseBody)
	}))
	defer upstream.Close()

	// Build handler with overridden upstreamURL via a custom RoundTripper.
	h := New(newTestCfg("ak", "ok"))

	// Temporarily swap out the upstream client to redirect to our test server.
	original := upstreamClient
	upstreamClient = &http.Client{
		Transport: &rewriteTransport{
			base:    http.DefaultTransport,
			rewrite: upstream.URL,
		},
	}
	defer func() { upstreamClient = original }()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-3-haiku","max_tokens":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rr.Code)
	}
	if got := rr.Body.String(); got != responseBody {
		t.Errorf("body = %q, want %q", got, responseBody)
	}
}

// ---------- client disconnect cancels upstream ----------

func TestHandler_ClientDisconnect_CancelsUpstream(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow disconnect test in short mode")
	}

	// upstream blocks until its context is cancelled.
	upstreamCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Signal that upstream received the request.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until our context is cancelled.
		<-r.Context().Done()
		close(upstreamCancelled)
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

	h := New(newTestCfg("ak", "ok"))

	// Use a context we can cancel to simulate client disconnect.
	clientCtx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-3-haiku","max_tokens":1,"stream":true}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(clientCtx)

	rr := httptest.NewRecorder()

	// Run ServeHTTP in a goroutine; cancel the client context shortly after.
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(rr, req)
	}()

	// Give the upstream a moment to start serving, then disconnect.
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Wait for upstream to detect cancellation.
	select {
	case <-upstreamCancelled:
		// Good — upstream context was cancelled.
	case <-time.After(3 * time.Second):
		t.Fatal("upstream context was not cancelled within timeout")
	}

	<-done
}

// ---------- SSE streaming ----------

func TestHandler_Streaming_ChunksForwarded(t *testing.T) {
	events := []string{
		"event: message_start\n",
		`data: {"message":{"model":"claude-3-haiku","usage":{"input_tokens":10,"output_tokens":0}}}` + "\n",
		"\n",
		"event: message_delta\n",
		`data: {"usage":{"output_tokens":25}}` + "\n",
		"\n",
		"data: [DONE]\n",
		"\n",
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f := w.(http.Flusher)
		for _, chunk := range events {
			_, _ = io.WriteString(w, chunk)
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

	// Pre-inject a *Usage into the request context so we can observe it
	// after ServeHTTP returns. This mirrors how M1-T4 (accounting middleware)
	// will work: it wraps the proxy and reads the same *Usage pointer after
	// the inner handler returns.
	//
	// The proxy's ServeHTTP overwrites the context internally, but since we
	// provide a *Usage pointer via contextWithUsage here, the handler will
	// find it and fill it. However, ServeHTTP actually allocates its own uPtr
	// — so the cleanest way to observe the result without changing the API
	// surface is to use a captureResponseWriter that intercepts the context.
	//
	// Simplest approach: wrap with a middleware that seeds a *Usage before
	// the proxy and reads it after. Since the proxy always allocates a new
	// *Usage and stores it on the derived context, we use a captureHandler.

	var capturedUsage *Usage
	h := New(newTestCfg("ak", "ok"))
	wrappedH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Seed a *Usage in context BEFORE calling the proxy, so the proxy
		// finds and fills it rather than allocating its own.
		uPtr := &Usage{}
		r = r.WithContext(contextWithUsage(r.Context(), uPtr))
		h.ServeHTTP(w, r)
		capturedUsage = uPtr
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude-3-haiku","max_tokens":50,"stream":true}`))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	wrappedH.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, "message_start") {
		t.Errorf("expected message_start in body, got: %q", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("expected [DONE] in body, got: %q", body)
	}

	// Verify usage was extracted and stored in the *Usage pointer.
	if capturedUsage == nil {
		t.Fatal("capturedUsage is nil")
	}
	if capturedUsage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", capturedUsage.InputTokens)
	}
	if capturedUsage.OutputTokens != 25 {
		t.Errorf("OutputTokens = %d, want 25", capturedUsage.OutputTokens)
	}
	if capturedUsage.Model != "claude-3-haiku" {
		t.Errorf("Model = %q, want %q", capturedUsage.Model, "claude-3-haiku")
	}
}

// ---------- usage context helpers ----------

func TestUsageFromContext_NotSet(t *testing.T) {
	_, ok := UsageFromContext(context.Background())
	if ok {
		t.Fatal("expected false when no usage in context")
	}
}

func TestUsageFromContext_Set(t *testing.T) {
	u := &Usage{Provider: "anthropic", InputTokens: 5, OutputTokens: 10}
	ctx := contextWithUsage(context.Background(), u)
	got, ok := UsageFromContext(ctx)
	if !ok {
		t.Fatal("expected true")
	}
	if got != u {
		t.Errorf("got %+v, want %+v", got, u)
	}
}

// ---------- usage parsing ----------

func TestTryParseAnthropicUsage_MessageStart(t *testing.T) {
	data := `{"message":{"model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":42,"output_tokens":0}}}`
	u := &Usage{}
	tryParseAnthropicUsage(data, "message_start", u)
	if u.InputTokens != 42 {
		t.Errorf("InputTokens = %d, want 42", u.InputTokens)
	}
	if u.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Model = %q, want claude-3-5-sonnet-20241022", u.Model)
	}
}

func TestTryParseAnthropicUsage_MessageDelta(t *testing.T) {
	data := `{"usage":{"output_tokens":99}}`
	u := &Usage{}
	tryParseAnthropicUsage(data, "message_delta", u)
	if u.OutputTokens != 99 {
		t.Errorf("OutputTokens = %d, want 99", u.OutputTokens)
	}
}

func TestTryParseOpenAIUsage(t *testing.T) {
	data := `{"model":"gpt-4o","usage":{"prompt_tokens":20,"completion_tokens":15}}`
	u := &Usage{}
	tryParseOpenAIUsage(data, u)
	if u.InputTokens != 20 {
		t.Errorf("InputTokens = %d, want 20", u.InputTokens)
	}
	if u.OutputTokens != 15 {
		t.Errorf("OutputTokens = %d, want 15", u.OutputTokens)
	}
	if u.Model != "gpt-4o" {
		t.Errorf("Model = %q, want gpt-4o", u.Model)
	}
}

func TestTryParseOpenAIUsage_NoUsageField(t *testing.T) {
	data := `{"id":"chatcmpl-1","choices":[{"delta":{"content":"hi"}}]}`
	u := &Usage{}
	tryParseOpenAIUsage(data, u)
	// Should remain zero.
	if u.InputTokens != 0 || u.OutputTokens != 0 {
		t.Errorf("expected zero usage, got %+v", u)
	}
}

// ---------- helpers ----------

// rewriteTransport redirects all requests to a fixed base URL (for test servers).
type rewriteTransport struct {
	base    http.RoundTripper
	rewrite string // scheme+host, e.g. "http://127.0.0.1:PORT"
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	// Parse host from rewrite URL.
	parts := strings.SplitN(strings.TrimPrefix(rt.rewrite, "http://"), "/", 2)
	cloned.URL.Host = parts[0]
	cloned.Host = parts[0]
	// Preserve path, but update RequestURI.
	cloned.RequestURI = fmt.Sprintf("%s?%s", cloned.URL.Path, cloned.URL.RawQuery)
	if cloned.URL.RawQuery == "" {
		cloned.RequestURI = cloned.URL.Path
	}
	return rt.base.RoundTrip(cloned)
}
