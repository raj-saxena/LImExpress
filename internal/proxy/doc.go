// Package proxy handles SSE streaming of LLM provider responses.
// It flushes each chunk immediately and cancels the upstream request on client disconnect.
package proxy
