package proxy

// Usage holds token counts extracted from a completed provider response.
// It is populated by the proxy after the full response is streamed.
type Usage struct {
	Provider     string
	Model        string
	InputTokens  int32
	OutputTokens int32
}
