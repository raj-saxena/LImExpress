package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/limexpress/gateway/internal/config"
)

// upstreamClient is the package-level HTTP client used for all upstream requests.
var upstreamClient = newUpstreamClient()

// usageContextKey is the key used to store *Usage in a request context.
type usageContextKey struct{}

// UsageFromContext retrieves the *Usage pointer stored in ctx by the proxy.
// M1-T4 calls this after next.ServeHTTP returns to read the populated usage data.
// Returns (nil, false) if the proxy has not stored usage.
func UsageFromContext(ctx context.Context) (*Usage, bool) {
	u, ok := ctx.Value(usageContextKey{}).(*Usage)
	return u, ok
}

// contextWithUsage stores a *Usage pointer in ctx.
func contextWithUsage(ctx context.Context, u *Usage) context.Context {
	return context.WithValue(ctx, usageContextKey{}, u)
}

// providerTarget describes the upstream endpoint and auth strategy for a provider.
type providerTarget struct {
	upstreamURL string
	provider    string // "anthropic" | "openai"
}

// routeRequest maps an incoming URL path to a providerTarget.
// Returns (target, true) on match, or (zero, false) for unknown paths.
func routeRequest(path string) (providerTarget, bool) {
	switch path {
	case "/v1/messages":
		return providerTarget{
			upstreamURL: "https://api.anthropic.com/v1/messages",
			provider:    "anthropic",
		}, true
	case "/v1/chat/completions":
		return providerTarget{
			upstreamURL: "https://api.openai.com/v1/chat/completions",
			provider:    "openai",
		}, true
	case "/v1/completions":
		return providerTarget{
			upstreamURL: "https://api.openai.com/v1/completions",
			provider:    "openai",
		}, true
	default:
		return providerTarget{}, false
	}
}

// handler is the core proxy handler.
type handler struct {
	cfg config.ProvidersConfig
}

// New returns an http.Handler that proxies requests to the appropriate upstream.
// It must be placed after VirtualKeyAuth (needs KeyAuthContext for provider selection).
func New(cfg config.ProvidersConfig) http.Handler {
	return &handler{cfg: cfg}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	target, ok := routeRequest(r.URL.Path)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Reuse an existing *Usage pointer if M1-T4 (or another wrapper) already
	// seeded one in the context. Otherwise allocate a new one.
	// This pointer pattern allows M1-T4 to seed a *Usage before calling the
	// proxy and then read the filled result after next.ServeHTTP returns,
	// without needing access to the proxy's internal derived context.
	uPtr, ok := UsageFromContext(r.Context())
	if !ok {
		uPtr = &Usage{}
	}
	uPtr.Provider = target.provider
	ctx := contextWithUsage(r.Context(), uPtr)
	r = r.WithContext(ctx)

	// Build the upstream request, cloning headers from client (minus auth).
	upstreamReq, err := http.NewRequestWithContext(ctx, r.Method, target.upstreamURL, r.Body)
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}

	// Copy request headers from client to upstream, excluding auth headers
	// that will be replaced with server-side provider credentials.
	for key, vals := range r.Header {
		lower := strings.ToLower(key)
		if lower == "x-api-key" || lower == "authorization" {
			continue
		}
		for _, v := range vals {
			upstreamReq.Header.Add(key, v)
		}
	}

	// Inject per-provider auth.
	switch target.provider {
	case "anthropic":
		upstreamReq.Header.Set("x-api-key", h.cfg.Anthropic.APIKey)
		upstreamReq.Header.Set("anthropic-version", "2023-06-01")
	case "openai":
		upstreamReq.Header.Set("Authorization", "Bearer "+h.cfg.OpenAI.APIKey)
	}

	// Execute upstream request.
	resp, err := upstreamClient.Do(upstreamReq)
	if err != nil {
		// If the context was cancelled (client disconnect), stop silently.
		if ctx.Err() != nil {
			return
		}
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers to client.
	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		h.streamSSE(w, r, resp, uPtr, target.provider)
	} else {
		// Non-streaming: copy entire body.
		_, _ = io.Copy(w, resp.Body)
	}
}

// anthropicMessageStartUsage is used to parse Anthropic's message_start event.
type anthropicMessageStartUsage struct {
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens  int32 `json:"input_tokens"`
			OutputTokens int32 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// anthropicMessageDeltaUsage is used to parse Anthropic's message_delta event.
type anthropicMessageDeltaUsage struct {
	Usage struct {
		OutputTokens int32 `json:"output_tokens"`
	} `json:"usage"`
}

// openAIChunkUsage is used to parse OpenAI streaming usage.
type openAIChunkUsage struct {
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     int32 `json:"prompt_tokens"`
		CompletionTokens int32 `json:"completion_tokens"`
	} `json:"usage"`
}

// streamSSE reads the SSE stream line by line, writes each line to the client,
// flushes after each line, and extracts usage data for M1-T4.
func (h *handler) streamSSE(w http.ResponseWriter, r *http.Request, resp *http.Response, uPtr *Usage, provider string) {
	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)

	// Track Anthropic event type across lines.
	var lastEventType string

	for scanner.Scan() {
		// Check for client disconnect.
		if r.Context().Err() != nil {
			return
		}

		line := scanner.Text()

		// Track SSE event type for Anthropic.
		if strings.HasPrefix(line, "event:") {
			lastEventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}

		// Write line + newline to client.
		_, _ = io.WriteString(w, line+"\n")
		if canFlush {
			flusher.Flush()
		}

		// Parse usage from data lines.
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" || data == "" {
				continue
			}

			switch provider {
			case "anthropic":
				tryParseAnthropicUsage(data, lastEventType, uPtr)
			case "openai":
				tryParseOpenAIUsage(data, uPtr)
			}
		}
	}

	// Write a final blank line to terminate the SSE stream properly.
	_, _ = io.WriteString(w, "\n")
	if canFlush {
		flusher.Flush()
	}
}

// tryParseAnthropicUsage attempts to extract token counts from Anthropic SSE events.
// It handles two event types:
//   - message_start: contains input_tokens (and initial output_tokens, usually 0)
//   - message_delta: contains final output_tokens count
func tryParseAnthropicUsage(data, eventType string, uPtr *Usage) {
	switch eventType {
	case "message_start":
		var ms anthropicMessageStartUsage
		if err := json.Unmarshal([]byte(data), &ms); err == nil {
			if ms.Message.Model != "" {
				uPtr.Model = ms.Message.Model
			}
			uPtr.InputTokens = ms.Message.Usage.InputTokens
			// Initial output_tokens from message_start is usually 0; we'll
			// overwrite with the definitive value from message_delta.
			if ms.Message.Usage.OutputTokens > 0 {
				uPtr.OutputTokens = ms.Message.Usage.OutputTokens
			}
		}
	case "message_delta":
		var md anthropicMessageDeltaUsage
		if err := json.Unmarshal([]byte(data), &md); err == nil {
			if md.Usage.OutputTokens > 0 {
				uPtr.OutputTokens = md.Usage.OutputTokens
			}
		}
	}
}

// tryParseOpenAIUsage attempts to extract token counts from OpenAI SSE chunks.
// OpenAI may include a usage object on the last data chunk before [DONE].
func tryParseOpenAIUsage(data string, uPtr *Usage) {
	// Quick check: only attempt parse if "usage" appears in the payload.
	if !bytes.Contains([]byte(data), []byte("usage")) {
		return
	}

	var chunk openAIChunkUsage
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}

	if chunk.Model != "" {
		uPtr.Model = chunk.Model
	}

	if chunk.Usage != nil {
		uPtr.InputTokens = chunk.Usage.PromptTokens
		uPtr.OutputTokens = chunk.Usage.CompletionTokens
	}
}
