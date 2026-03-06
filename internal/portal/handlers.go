package portal

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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
// The authenticated user's email is not yet available (auth wired in a later task);
// an empty string causes Nav to display the sign-in link instead.
func (h *Handler) indexHandler(w http.ResponseWriter, r *http.Request) {
	// TODO(auth): replace "" with the session user email once OIDC is wired.
	component := templates.Index("")
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
