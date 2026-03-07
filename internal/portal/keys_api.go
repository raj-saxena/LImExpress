package portal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/keys"
	"github.com/limexpress/gateway/internal/portal/auth"
)

// KeyLifecycleHandler exposes portal key lifecycle endpoints.
type KeyLifecycleHandler struct {
	q db.Querier
}

// NewKeyLifecycleHandler creates a new key lifecycle API handler.
func NewKeyLifecycleHandler(q db.Querier) *KeyLifecycleHandler {
	return &KeyLifecycleHandler{q: q}
}

// RegisterRoutes mounts M2-T3 JSON endpoints.
// Note: GET /portal/keys is handled by the HTML portal handler (keysPageHandler)
// to serve the key management UI. Only JSON mutation endpoints are registered here.
func (h *KeyLifecycleHandler) RegisterRoutes(r chi.Router) {
	r.Post("/portal/keys", h.createKey)
	r.Delete("/portal/keys/{id}", h.revokeKey)
}

func (h *KeyLifecycleHandler) listKeys(w http.ResponseWriter, r *http.Request) {
	user, org, ok := requirePortalContext(w, r)
	if !ok {
		return
	}

	data := make([]portalKey, 0)
	if org.Role == "org_admin" {
		rows, err := h.q.ListKeysByOrg(r.Context(), org.OrgID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, row := range rows {
			data = append(data, portalKey{
				ID:         uuidToString(row.ID),
				UserID:     uuidToString(row.UserID),
				TeamID:     optionalUUID(row.TeamID),
				Prefix:     row.Prefix,
				Status:     row.Status,
				CreatedAt:  timeToRFC3339(row.CreatedAt),
				LastUsedAt: optionalTime(row.LastUsedAt),
				RevokedAt:  optionalTime(row.RevokedAt),
			})
		}
	} else {
		rows, err := h.q.ListVirtualKeysByUser(r.Context(), db.ListVirtualKeysByUserParams{
			UserID: user.UserID,
			OrgID:  org.OrgID,
		})
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, row := range rows {
			data = append(data, portalKey{
				ID:         uuidToString(row.ID),
				UserID:     uuidToString(row.UserID),
				TeamID:     optionalUUID(row.TeamID),
				Prefix:     row.Prefix,
				Status:     row.Status,
				CreatedAt:  timeToRFC3339(row.CreatedAt),
				LastUsedAt: optionalTime(row.LastUsedAt),
				RevokedAt:  optionalTime(row.RevokedAt),
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *KeyLifecycleHandler) createKey(w http.ResponseWriter, r *http.Request) {
	user, org, ok := requirePortalContext(w, r)
	if !ok {
		return
	}
	if org.Role != "org_admin" {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	plaintext, sha256hex, prefix, err := keys.Generate()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	created, err := h.q.CreateVirtualKey(r.Context(), db.CreateVirtualKeyParams{
		OrgID:   org.OrgID,
		UserID:  user.UserID,
		TeamID:  pgtype.UUID{},
		KeyHash: sha256hex,
		Prefix:  prefix,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"data": map[string]any{
			"id":         uuidToString(created.ID),
			"prefix":     created.Prefix,
			"status":     created.Status,
			"created_at": timeToRFC3339(created.CreatedAt),
			"key":        plaintext,
		},
	})
}

func (h *KeyLifecycleHandler) revokeKey(w http.ResponseWriter, r *http.Request) {
	_, org, ok := requirePortalContext(w, r)
	if !ok {
		return
	}
	if org.Role != "org_admin" {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	keyID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid key id")
		return
	}

	if err := h.q.RevokeVirtualKey(r.Context(), db.RevokeVirtualKeyParams{ID: keyID, OrgID: org.OrgID}); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type portalKey struct {
	ID         string  `json:"id"`
	UserID     string  `json:"user_id"`
	TeamID     *string `json:"team_id"`
	Prefix     string  `json:"prefix"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func requirePortalContext(w http.ResponseWriter, r *http.Request) (*auth.UserContext, *auth.OrgContext, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return nil, nil, false
	}
	org, ok := auth.OrgFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return nil, nil, false
	}
	return user, org, true
}

func parseUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil || !u.Valid {
		return pgtype.UUID{}, fmt.Errorf("invalid uuid: %q", s)
	}
	return u, nil
}

func optionalUUID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := uuidToString(u)
	return &s
}

func optionalTime(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	s := timeToRFC3339(t)
	return &s
}

func timeToRFC3339(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
