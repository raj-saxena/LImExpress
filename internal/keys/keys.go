package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const keyPrefix = "sk_vkey_"

// Generate creates a new virtual key. It returns:
//   - plaintext: the full key to show the user once (e.g. "sk_vkey_...")
//   - sha256hex: hex(sha256(plaintext)) — stored in DB as key_hash for fast lookup
//   - prefix:    first 12 chars of plaintext — stored in DB for display
func Generate() (plaintext, sha256hex, prefix string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", "", fmt.Errorf("keys: generate random bytes: %w", err)
	}
	// base64url without padding
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	plaintext = keyPrefix + encoded

	sha256hex = HashForLookup(plaintext)

	// prefix is the first 12 characters of the full key (includes "sk_vkey_" portion)
	if len(plaintext) < 12 {
		prefix = plaintext
	} else {
		prefix = plaintext[:12]
	}
	return plaintext, sha256hex, prefix, nil
}

// HashForLookup returns hex(sha256(plaintext)). This is the value stored in the
// DB as key_hash and used to look up keys at request time without bcrypt overhead.
// Never log this value — it is a deterministic derivation of the secret key.
func HashForLookup(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// ParseHeader extracts the virtual key from the request. It accepts:
//   - x-api-key: <key>          (Anthropic style)
//   - Authorization: Bearer <key> (OpenAI style)
//
// Returns an error if neither header is present or the extracted value is empty.
func ParseHeader(r *http.Request) (string, error) {
	// Anthropic style takes priority
	if v := r.Header.Get("x-api-key"); v != "" {
		return v, nil
	}
	// OpenAI style
	auth := r.Header.Get("Authorization")
	if auth != "" {
		after, found := strings.CutPrefix(auth, "Bearer ")
		if found && after != "" {
			return after, nil
		}
	}
	return "", errors.New("keys: no api key in request headers")
}
