package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/keys"
)

type authMockQuerier struct {
	db.Querier

	mu          sync.Mutex
	keyRow      db.VirtualKey
	getErr      error
	gotHash     string
	updateErr   error
	updateCalls []db.UpdateVirtualKeyLastUsedParams
	updateCh    chan struct{}
}

func newAuthMockQuerier() *authMockQuerier {
	return &authMockQuerier{updateCh: make(chan struct{}, 1)}
}

func (m *authMockQuerier) GetVirtualKeyByHash(_ context.Context, hash string) (db.VirtualKey, error) {
	m.mu.Lock()
	m.gotHash = hash
	m.mu.Unlock()
	if m.getErr != nil {
		return db.VirtualKey{}, m.getErr
	}
	return m.keyRow, nil
}

func (m *authMockQuerier) UpdateVirtualKeyLastUsed(_ context.Context, arg db.UpdateVirtualKeyLastUsedParams) error {
	m.mu.Lock()
	m.updateCalls = append(m.updateCalls, arg)
	m.mu.Unlock()
	select {
	case m.updateCh <- struct{}{}:
	default:
	}
	return m.updateErr
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic("invalid UUID " + s + ": " + err.Error())
	}
	return u
}

func TestVirtualKeyAuth_Success(t *testing.T) {
	q := newAuthMockQuerier()
	q.keyRow = db.VirtualKey{
		ID:     mustUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"),
		OrgID:  mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		UserID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		TeamID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		Status: "active",
	}

	var sawContext bool
	h := VirtualKeyAuth(q)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kac, ok := FromContext(r.Context())
		if !ok {
			t.Fatal("expected auth context")
		}
		sawContext = true
		if kac.KeyID != q.keyRow.ID || kac.OrgID != q.keyRow.OrgID || kac.UserID != q.keyRow.UserID || kac.TeamID != q.keyRow.TeamID {
			t.Fatalf("unexpected KeyAuthContext: %+v", kac)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	const plaintext = "sk_vkey_test_success"
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Header.Set("x-api-key", plaintext)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want 204", rr.Code)
	}
	if !sawContext {
		t.Fatal("next handler was not called")
	}

	select {
	case <-q.updateCh:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UpdateVirtualKeyLastUsed")
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.gotHash != keys.HashForLookup(plaintext) {
		t.Fatalf("GetVirtualKeyByHash hash=%q, want %q", q.gotHash, keys.HashForLookup(plaintext))
	}
	if len(q.updateCalls) != 1 {
		t.Fatalf("UpdateVirtualKeyLastUsed calls=%d, want 1", len(q.updateCalls))
	}
	if q.updateCalls[0].ID != q.keyRow.ID || q.updateCalls[0].OrgID != q.keyRow.OrgID {
		t.Fatalf("unexpected UpdateVirtualKeyLastUsed params: %+v", q.updateCalls[0])
	}
}

func TestVirtualKeyAuth_UnauthorizedCases(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*authMockQuerier, *http.Request)
	}{
		{
			name: "missing credentials",
			setup: func(_ *authMockQuerier, _ *http.Request) {
			},
		},
		{
			name: "malformed authorization header",
			setup: func(_ *authMockQuerier, r *http.Request) {
				r.Header.Set("Authorization", "Basic abc")
			},
		},
		{
			name: "db miss",
			setup: func(q *authMockQuerier, r *http.Request) {
				r.Header.Set("Authorization", "Bearer sk_vkey_test")
				q.getErr = pgx.ErrNoRows
			},
		},
		{
			name: "db error",
			setup: func(q *authMockQuerier, r *http.Request) {
				r.Header.Set("Authorization", "Bearer sk_vkey_test")
				q.getErr = errors.New("connection reset")
			},
		},
		{
			name: "revoked key",
			setup: func(q *authMockQuerier, r *http.Request) {
				r.Header.Set("x-api-key", "sk_vkey_test")
				q.keyRow = db.VirtualKey{
					ID:     mustUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"),
					OrgID:  mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
					UserID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
					Status: "revoked",
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := newAuthMockQuerier()
			called := false
			h := VirtualKeyAuth(q)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
			tc.setup(q, req)
			rr := httptest.NewRecorder()

			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d, want 401", rr.Code)
			}
			if called {
				t.Fatal("next handler should not be called")
			}
			if got := rr.Body.String(); got != "{\"error\":\"unauthorized\"}\n" {
				t.Fatalf("body=%q, want unauthorized json", got)
			}
		})
	}
}

func TestVirtualKeyAuth_LastUsedUpdateFailureDoesNotBlock(t *testing.T) {
	q := newAuthMockQuerier()
	q.updateErr = errors.New("write failed")
	q.keyRow = db.VirtualKey{
		ID:     mustUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"),
		OrgID:  mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		UserID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		Status: "active",
	}

	called := false
	h := VirtualKeyAuth(q)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer sk_vkey_test")
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rr.Code)
	}
	if !called {
		t.Fatal("next handler was not called")
	}
}
