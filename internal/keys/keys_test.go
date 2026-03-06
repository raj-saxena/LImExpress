package keys_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/limexpress/gateway/internal/keys"
)

func TestGenerate(t *testing.T) {
	plaintext, sha256hex, prefix, err := keys.Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// Key must start with the well-known prefix
	if !strings.HasPrefix(plaintext, "sk_vkey_") {
		t.Errorf("plaintext %q does not start with sk_vkey_", plaintext)
	}

	// Length must be reasonable: "sk_vkey_" (8) + base64url(32 bytes) = 8+43 = 51
	if len(plaintext) < 40 {
		t.Errorf("plaintext length %d is suspiciously short", len(plaintext))
	}

	// sha256hex must be exactly 64 hex characters
	if len(sha256hex) != 64 {
		t.Errorf("sha256hex length = %d, want 64", len(sha256hex))
	}
	for _, c := range sha256hex {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("sha256hex %q contains non-lowercase-hex char %q", sha256hex, c)
			break
		}
	}

	// prefix must be the first 12 chars of plaintext
	if len(plaintext) < 12 {
		t.Fatalf("plaintext too short to have 12-char prefix")
	}
	if prefix != plaintext[:12] {
		t.Errorf("prefix = %q, want %q", prefix, plaintext[:12])
	}
}

func TestHashForLookup(t *testing.T) {
	const input = "sk_vkey_somefakekey"

	h1 := keys.HashForLookup(input)
	h2 := keys.HashForLookup(input)

	// Deterministic: same input → same hash
	if h1 != h2 {
		t.Errorf("HashForLookup is not deterministic: %q != %q", h1, h2)
	}

	// Different input → different hash
	h3 := keys.HashForLookup("sk_vkey_differentkey")
	if h1 == h3 {
		t.Errorf("HashForLookup collision between different inputs")
	}

	// Must be 64 hex chars
	if len(h1) != 64 {
		t.Errorf("HashForLookup length = %d, want 64", len(h1))
	}
}

func TestParseHeader_AnthropicStyle(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("x-api-key", "sk_vkey_abc")

	got, err := keys.ParseHeader(req)
	if err != nil {
		t.Fatalf("ParseHeader() error: %v", err)
	}
	if got != "sk_vkey_abc" {
		t.Errorf("ParseHeader() = %q, want %q", got, "sk_vkey_abc")
	}
}

func TestParseHeader_OpenAIStyle(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer sk_vkey_abc")

	got, err := keys.ParseHeader(req)
	if err != nil {
		t.Fatalf("ParseHeader() error: %v", err)
	}
	if got != "sk_vkey_abc" {
		t.Errorf("ParseHeader() = %q, want %q", got, "sk_vkey_abc")
	}
}

func TestParseHeader_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	_, err := keys.ParseHeader(req)
	if err == nil {
		t.Error("ParseHeader() expected error for missing headers, got nil")
	}
}

func TestParseHeader_AnthropicTakesPriorityOverBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("x-api-key", "sk_vkey_anthropic")
	req.Header.Set("Authorization", "Bearer sk_vkey_openai")

	got, err := keys.ParseHeader(req)
	if err != nil {
		t.Fatalf("ParseHeader() error: %v", err)
	}
	// x-api-key takes priority
	if got != "sk_vkey_anthropic" {
		t.Errorf("ParseHeader() = %q, want %q (x-api-key should win)", got, "sk_vkey_anthropic")
	}
}
