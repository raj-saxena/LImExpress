package portal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/limexpress/gateway/internal/db"
)

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
