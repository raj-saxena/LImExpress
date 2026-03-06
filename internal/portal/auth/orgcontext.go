package auth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/limexpress/gateway/internal/db"
)

const orgContextKey contextKey = iota + 1

// OrgContext holds the active org for the current portal session.
type OrgContext struct {
	OrgID pgtype.UUID
	Name  string
	Role  string // "member" | "org_admin"
}

// OrgFromContext retrieves OrgContext from the request context.
// Returns false if no org is present in the context.
func OrgFromContext(ctx context.Context) (*OrgContext, bool) {
	o, ok := ctx.Value(orgContextKey).(*OrgContext)
	return o, ok
}

// OrgMiddleware resolves the active org for the request and injects OrgContext.
// It must run AFTER RequireAuth (needs UserContext).
// Resolution order:
//  1. `X-Active-Org` request header (UUID string) — useful for API clients
//  2. `active_org_id` value in the session cookie
//  3. First org returned by GetUserOrgs (auto-select if only one or as fallback)
//
// If the user belongs to no orgs, redirects to /portal/access-denied.
// If the resolved org_id is not in the user's membership list, falls back to first org.
func (h *Handler) OrgMiddleware(q db.Querier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				http.Redirect(w, r, "/auth/login", http.StatusFound)
				return
			}

			orgs, err := q.GetUserOrgs(r.Context(), user.UserID)
			if err != nil {
				http.Error(w, "database error", http.StatusInternalServerError)
				return
			}

			if len(orgs) == 0 {
				http.Redirect(w, r, "/portal/access-denied", http.StatusFound)
				return
			}

			// Build a lookup map from org UUID string to row index for fast validation.
			orgByID := make(map[string]int, len(orgs))
			for i, o := range orgs {
				orgByID[uuidToString(o.ID)] = i
			}

			// Resolution order: header → session → first org.
			resolvedIdx := 0 // default: first org

			// 1. X-Active-Org header.
			if hdr := r.Header.Get("X-Active-Org"); hdr != "" {
				if idx, found := orgByID[hdr]; found {
					resolvedIdx = idx
				}
				// If header value is not in membership, fall through to session / first org.
			} else {
				// 2. Session cookie.
				sess, err := h.store.Get(r, sessionName)
				if err != nil {
					sess = sessions.NewSession(h.store, sessionName)
				}
				if sessionOrgID, ok := sess.Values["active_org_id"].(string); ok && sessionOrgID != "" {
					if idx, found := orgByID[sessionOrgID]; found {
						resolvedIdx = idx
					}
				}
			}

			chosen := orgs[resolvedIdx]
			orgCtx := &OrgContext{
				OrgID: chosen.ID,
				Name:  chosen.Name,
				Role:  chosen.Role,
			}

			ctx := context.WithValue(r.Context(), orgContextKey, orgCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SwitchOrgHandler handles POST /portal/switch-org.
// Body (form): org_id=<uuid>
// Validates that the org_id is in the user's membership list, saves it to session,
// and redirects back to /portal (or Referer header if same origin).
func (h *Handler) SwitchOrgHandler(q db.Querier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		orgIDStr := r.FormValue("org_id")
		if orgIDStr == "" {
			http.Error(w, "org_id is required", http.StatusBadRequest)
			return
		}

		orgs, err := q.GetUserOrgs(r.Context(), user.UserID)
		if err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		// Validate org_id is in user's membership list.
		found := false
		for _, o := range orgs {
			if uuidToString(o.ID) == orgIDStr {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "forbidden: org not in membership", http.StatusForbidden)
			return
		}

		sess, err := h.store.Get(r, sessionName)
		if err != nil {
			sess = sessions.NewSession(h.store, sessionName)
		}
		sess.Values["active_org_id"] = orgIDStr
		if err := sess.Save(r, w); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// Redirect to Referer if same origin, otherwise /portal.
		redirect := "/portal"
		if referer := r.Header.Get("Referer"); referer != "" {
			if u, err := url.Parse(referer); err == nil {
				// Only trust same-origin referers (no host, or matching host).
				if u.Host == "" || u.Host == r.Host {
					redirect = referer
				}
			}
		}

		http.Redirect(w, r, redirect, http.StatusFound)
	}
}
