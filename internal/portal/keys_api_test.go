package portal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/keys"
	"github.com/limexpress/gateway/internal/portal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keyLifecycleMock struct {
	db.Querier

	listByOrgRows  []db.ListKeysByOrgRow
	listByOrgErr   error
	listByOrgOrgID pgtype.UUID

	listByUserRows  []db.ListVirtualKeysByUserRow
	listByUserErr   error
	listByUserParam db.ListVirtualKeysByUserParams

	createArg db.CreateVirtualKeyParams
	createRow db.VirtualKey
	createErr error

	revokeArg db.RevokeVirtualKeyParams
	revokeErr error
}

func (m *keyLifecycleMock) ListKeysByOrg(_ context.Context, orgID pgtype.UUID) ([]db.ListKeysByOrgRow, error) {
	m.listByOrgOrgID = orgID
	return m.listByOrgRows, m.listByOrgErr
}

func (m *keyLifecycleMock) ListVirtualKeysByUser(_ context.Context, arg db.ListVirtualKeysByUserParams) ([]db.ListVirtualKeysByUserRow, error) {
	m.listByUserParam = arg
	return m.listByUserRows, m.listByUserErr
}

func (m *keyLifecycleMock) CreateVirtualKey(_ context.Context, arg db.CreateVirtualKeyParams) (db.VirtualKey, error) {
	m.createArg = arg
	if m.createErr != nil {
		return db.VirtualKey{}, m.createErr
	}
	return m.createRow, nil
}

func (m *keyLifecycleMock) RevokeVirtualKey(_ context.Context, arg db.RevokeVirtualKeyParams) error {
	m.revokeArg = arg
	return m.revokeErr
}

// Note: TestKeyLifecycle_ListKeys_AdminSeesOrg, TestKeyLifecycle_ListKeys_MemberSeesOwn,
// and TestKeyLifecycle_List_InternalError have been removed.
// GET /portal/keys is now handled by the HTML portal handler (keysPageHandler);
// list behaviour is tested in keys_ui_test.go.

func TestKeyLifecycle_Create_AdminOnly(t *testing.T) {
	mock := &keyLifecycleMock{}
	r := chi.NewRouter()
	NewKeyLifecycleHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/portal/keys", nil)
	req = withPortalContext(req, "member")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestKeyLifecycle_Create_Success(t *testing.T) {
	mock := &keyLifecycleMock{createRow: db.VirtualKey{
		ID:        mustUUID("22222222-2222-2222-2222-222222222222"),
		Prefix:    "sk_vkey_1234",
		Status:    "active",
		CreatedAt: mustTS("2026-03-06T12:00:00Z"),
	}}
	r := chi.NewRouter()
	NewKeyLifecycleHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodPost, "/portal/keys", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, mustUUID("11111111-1111-1111-1111-111111111111"), mock.createArg.OrgID)
	assert.Equal(t, mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), mock.createArg.UserID)
	assert.False(t, mock.createArg.TeamID.Valid)

	var body struct {
		Data struct {
			ID  string `json:"id"`
			Key string `json:"key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", body.Data.ID)
	assert.NotEmpty(t, body.Data.Key)
	assert.Equal(t, keys.HashForLookup(body.Data.Key), mock.createArg.KeyHash)
}

func TestKeyLifecycle_Revoke_AdminOnly(t *testing.T) {
	mock := &keyLifecycleMock{}
	r := chi.NewRouter()
	NewKeyLifecycleHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodDelete, "/portal/keys/22222222-2222-2222-2222-222222222222", nil)
	req = withPortalContext(req, "member")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestKeyLifecycle_Revoke_Success(t *testing.T) {
	mock := &keyLifecycleMock{}
	r := chi.NewRouter()
	NewKeyLifecycleHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodDelete, "/portal/keys/22222222-2222-2222-2222-222222222222", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, mustUUID("22222222-2222-2222-2222-222222222222"), mock.revokeArg.ID)
	assert.Equal(t, mustUUID("11111111-1111-1111-1111-111111111111"), mock.revokeArg.OrgID)
}

func TestKeyLifecycle_Revoke_BadID(t *testing.T) {
	mock := &keyLifecycleMock{}
	r := chi.NewRouter()
	NewKeyLifecycleHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodDelete, "/portal/keys/not-a-uuid", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func withPortalContext(req *http.Request, role string) *http.Request {
	ctx := auth.ContextWithUser(req.Context(), &auth.UserContext{
		UserID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Email:  "user@example.com",
	})
	ctx = auth.ContextWithOrg(ctx, &auth.OrgContext{
		OrgID: mustUUID("11111111-1111-1111-1111-111111111111"),
		Role:  role,
		Name:  "Acme",
	})
	return req.WithContext(ctx)
}

func mustTS(s string) pgtype.Timestamptz {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
