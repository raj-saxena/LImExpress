package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/sessions"
)

// TestUserFromContext_Empty verifies that UserFromContext returns false
// when no user has been injected into the context.
func TestUserFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	u, ok := UserFromContext(ctx)
	if ok {
		t.Fatalf("expected ok=false for empty context, got ok=true with %+v", u)
	}
	if u != nil {
		t.Fatalf("expected nil user for empty context, got %+v", u)
	}
}

// TestRequireAuth_NoSession verifies that RequireAuth redirects to /auth/login
// when no valid session cookie is present in the request.
func TestRequireAuth_NoSession(t *testing.T) {
	store := sessions.NewCookieStore([]byte("test-secret-32bytes-for-testing!"))
	h := &Handler{
		store: store,
	}

	// A sentinel handler that must NOT be called if auth fails.
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called when session is missing")
		w.WriteHeader(http.StatusOK)
	})

	middleware := h.RequireAuth(sentinel)

	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, rec.Code)
	}
	location := rec.Header().Get("Location")
	if location != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", location)
	}
}
