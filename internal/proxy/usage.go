package proxy

import "context"

// Usage holds token counts extracted from a completed provider response.
// It is populated by the proxy after the full response is streamed.
type Usage struct {
	Provider     string
	Model        string
	InputTokens  int32
	OutputTokens int32
}

// ContextWithUsage seeds a *Usage pointer into ctx so the proxy can populate
// it during streaming. The accounting middleware calls this before invoking the
// proxy, then reads the filled pointer after ServeHTTP returns.
func ContextWithUsage(ctx context.Context, u *Usage) context.Context {
	return contextWithUsage(ctx, u)
}
