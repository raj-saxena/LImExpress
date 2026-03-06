package portal

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/limexpress/gateway/internal/portal/auth"
	"github.com/limexpress/gateway/internal/portal/templates"
)

// Handler wraps portal template rendering.
type Handler struct{}

// New returns a new portal Handler.
func New() *Handler { return &Handler{} }

// RegisterRoutes mounts all portal routes on the given chi router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/", h.indexHandler)
	r.Get("/portal", h.indexHandler)
	r.Get("/auth/login-page", h.loginPageHandler)
}

// indexHandler renders the portal dashboard.
// It reads the authenticated user's email from the request context when available.
// If no session exists (unauthenticated request), an empty string is passed and
// Nav renders the sign-in link instead.
func (h *Handler) indexHandler(w http.ResponseWriter, r *http.Request) {
	userEmail := ""
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userEmail = u.Email
	}
	component := templates.Index(userEmail)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// loginPageHandler renders the sign-in page.
func (h *Handler) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	component := templates.Login()
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
