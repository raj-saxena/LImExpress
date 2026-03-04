package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/keys"
)

// KeyAuthContext holds the resolved identity for an authenticated request.
type KeyAuthContext struct {
	OrgID  pgtype.UUID
	UserID pgtype.UUID
	TeamID pgtype.UUID // zero value if no team
	KeyID  pgtype.UUID
}

type contextKey int

const keyAuthContextKey contextKey = 0

// FromContext retrieves the KeyAuthContext injected by VirtualKeyAuth.
// Returns (nil, false) if the middleware has not run or authentication failed.
func FromContext(ctx context.Context) (*KeyAuthContext, bool) {
	v, ok := ctx.Value(keyAuthContextKey).(*KeyAuthContext)
	return v, ok
}

// errResponse writes a JSON error body with the given HTTP status code.
// It intentionally uses a generic message to avoid enumerating key states
// (not-found vs revoked look identical to the caller).
func errResponse(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Ignoring encode error — if we can't write the body the status code is enough.
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}

// VirtualKeyAuth returns an http.Handler middleware that authenticates every
// request using a virtual key extracted from standard API key headers.
//
// Lookup flow:
//  1. Extract plaintext key via keys.ParseHeader (x-api-key or Authorization: Bearer).
//  2. Compute sha256hex = keys.HashForLookup(plaintext) for deterministic DB lookup.
//  3. Call q.GetVirtualKeyByHash — if not found, return 401.
//  4. Reject any key whose status is not "active" with 401.
//  5. Fire-and-forget UpdateVirtualKeyLastUsed (failure does not block the request).
//  6. Inject KeyAuthContext into the request context and call next.
//
// Security invariants:
//   - The plaintext key and its sha256hex are never logged.
//   - "not found" and "revoked" responses are identical to prevent enumeration.
func VirtualKeyAuth(q db.Querier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			plaintext, err := keys.ParseHeader(r)
			if err != nil {
				errResponse(w, http.StatusUnauthorized)
				return
			}

			// Derive fast lookup token — never log this value.
			sha256hex := keys.HashForLookup(plaintext)

			keyRow, err := q.GetVirtualKeyByHash(r.Context(), sha256hex)
			if err != nil {
				// Not found or DB error — return identical 401 to prevent enumeration.
				errResponse(w, http.StatusUnauthorized)
				return
			}

			if keyRow.Status != "active" {
				errResponse(w, http.StatusUnauthorized)
				return
			}

			// Fire-and-forget: update last-used timestamp.
			// We do not block or fail the request on accounting errors.
			go func() {
				_ = q.UpdateVirtualKeyLastUsed(context.Background(), keyRow.ID)
			}()

			kac := &KeyAuthContext{
				OrgID:  keyRow.OrgID,
				UserID: keyRow.UserID,
				TeamID: keyRow.TeamID,
				KeyID:  keyRow.ID,
			}

			ctx := context.WithValue(r.Context(), keyAuthContextKey, kac)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
