package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/portal/auth"
)

// newTestRouterWithQuerier builds a chi router with portal routes and a real querier.
func newTestRouterWithQuerier(q db.Querier) *chi.Mux {
	store := sessions.NewCookieStore([]byte("test-secret-32bytes-for-testing!"))
	authHandler := auth.NewWithStore(store, nil)
	h := New(authHandler, q)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// withKeysContext injects user and org context directly into the request, bypassing
// session auth. This lets us test handler logic without a real session store.
func withKeysContext(req *http.Request, role string) *http.Request {
	ctx := auth.ContextWithUser(req.Context(), &auth.UserContext{
		UserID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Email:  "user@example.com",
	})
	ctx = auth.ContextWithOrg(ctx, &auth.OrgContext{
		OrgID: mustUUID("11111111-1111-1111-1111-111111111111"),
		Role:  role,
		Name:  "Acme Corp",
	})
	return req.WithContext(ctx)
}

// keysPageMock implements db.Querier for keysPageHandler tests.
type keysPageMock struct {
	db.Querier
	listByOrgRows   []db.ListKeysByOrgRow
	listByOrgErr    error
	listByUserRows  []db.ListVirtualKeysByUserRow
	listByUserErr   error
}

func (m *keysPageMock) ListKeysByOrg(_ interface{ Done() }, orgID pgtype.UUID) ([]db.ListKeysByOrgRow, error) {
	return m.listByOrgRows, m.listByOrgErr
}

func (m *keysPageMock) ListVirtualKeysByUser(_ interface{ Done() }, arg db.ListVirtualKeysByUserParams) ([]db.ListVirtualKeysByUserRow, error) {
	return m.listByUserRows, m.listByUserErr
}

// TestKeysPage_AdminRenders verifies that an admin user gets a 200 response
// with the keys table and Create Key button rendered.
func TestKeysPage_AdminRenders(t *testing.T) {
	mock := &keyLifecycleMock{
		listByOrgRows: []db.ListKeysByOrgRow{{
			ID:        mustUUID("00000000-0000-0000-0000-000000000001"),
			UserID:    mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			Prefix:    "sk_vkey_abcd",
			Status:    "active",
			CreatedAt: mustTS("2026-03-06T10:00:00Z"),
		}},
	}

	h := &Handler{
		authHandler: nil,
		querier:     mock,
	}

	req := httptest.NewRequest(http.MethodGet, "/portal/keys", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()

	h.keysPageHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "sk_vkey_abcd") {
		t.Error("expected prefix 'sk_vkey_abcd' in response body")
	}
	if !strings.Contains(body, "Create Key") {
		t.Error("expected 'Create Key' button for admin")
	}
	if !strings.Contains(body, "hx-post") {
		t.Error("expected HTMX post attribute for Create Key form")
	}
}

// TestKeysPage_MemberRenders verifies that a member user gets a 200 response
// with the keys table but no Create Key button or Revoke actions.
func TestKeysPage_MemberRenders(t *testing.T) {
	mock := &keyLifecycleMock{
		listByUserRows: []db.ListVirtualKeysByUserRow{{
			ID:        mustUUID("00000000-0000-0000-0000-000000000002"),
			UserID:    mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			Prefix:    "sk_vkey_member",
			Status:    "active",
			CreatedAt: mustTS("2026-03-06T10:00:00Z"),
		}},
	}

	h := &Handler{
		authHandler: nil,
		querier:     mock,
	}

	req := httptest.NewRequest(http.MethodGet, "/portal/keys", nil)
	req = withPortalContext(req, "member")
	rec := httptest.NewRecorder()

	h.keysPageHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "sk_vkey_member") {
		t.Error("expected prefix 'sk_vkey_member' in response body")
	}
	if strings.Contains(body, "Create Key") {
		t.Error("expected no 'Create Key' button for member")
	}
	if strings.Contains(body, "hx-delete") {
		t.Error("expected no Revoke buttons for member")
	}
}

// TestKeysPage_EmptyState verifies the empty state is rendered when there are no keys.
func TestKeysPage_EmptyState(t *testing.T) {
	mock := &keyLifecycleMock{
		listByOrgRows: []db.ListKeysByOrgRow{},
	}

	h := &Handler{
		authHandler: nil,
		querier:     mock,
	}

	req := httptest.NewRequest(http.MethodGet, "/portal/keys", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()

	h.keysPageHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "No API keys yet") {
		t.Error("expected empty state message in response body")
	}
}
