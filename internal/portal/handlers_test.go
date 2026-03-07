package portal_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/limexpress/gateway/internal/portal"
	portalauth "github.com/limexpress/gateway/internal/portal/auth"
)

// newTestRouter builds a chi router with portal routes wired up using a
// test-only auth handler (no real OIDC provider — session validation only).
func newTestRouter() *chi.Mux {
	store := sessions.NewCookieStore([]byte("test-secret-32bytes-for-testing!"))
	authHandler := portalauth.NewWithStore(store, nil)
	h := portal.New(authHandler, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func TestLoginPage_Renders(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/auth/login-page", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty body")
	}
}

func TestLoginPage_SignedOutFlash(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/auth/login-page?signed_out=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(strings.ToLower(body), "signed out") {
		t.Errorf("expected 'signed out' text in response body")
	}
}

func TestRootRedirect(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login-page" {
		t.Fatalf("expected redirect to /auth/login-page, got %q", loc)
	}
}

func TestPortalDashboard_Unauthenticated(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

func TestAccessDenied_Unauthenticated(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/portal/access-denied", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

func TestUsagePage_Unauthenticated(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/portal/usage", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}
