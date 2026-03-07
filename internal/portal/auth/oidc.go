package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/oauth2"

	"github.com/limexpress/gateway/internal/db"
)

const (
	sessionName   = "limexpress_session"
	sessionMaxAge = 8 * 60 * 60 // 8 hours in seconds
	stateLen      = 16           // random bytes for CSRF state
)

// Config holds OIDC configuration.
type Config struct {
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	SessionSecret string // hex-encoded 32 bytes used to sign the session cookie
}

// Handler holds the OIDC provider and oauth2 config.
type Handler struct {
	provider *gooidc.Provider
	oauth2   oauth2.Config
	store    *sessions.CookieStore
	db       db.Querier
}

// New initializes the OIDC provider by fetching Google's discovery document.
// Returns an error if the provider cannot be initialized or the session secret is invalid.
func New(ctx context.Context, cfg Config, querier db.Querier) (*Handler, error) {
	secretBytes, err := hex.DecodeString(cfg.SessionSecret)
	if err != nil {
		return nil, fmt.Errorf("auth: session secret must be hex-encoded: %w", err)
	}
	if len(secretBytes) < 32 {
		return nil, fmt.Errorf("auth: session secret must be at least 32 bytes (got %d)", len(secretBytes))
	}

	provider, err := gooidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("auth: initializing OIDC provider: %w", err)
	}

	store := sessions.NewCookieStore(secretBytes)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	return &Handler{
		provider: provider,
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{gooidc.ScopeOpenID, "email", "profile"},
		},
		store: store,
		db:    querier,
	}, nil
}

// NewWithStore creates a Handler with a pre-configured session store and querier.
// Only used in tests where a real OIDC provider is not available.
func NewWithStore(store *sessions.CookieStore, querier db.Querier) *Handler {
	return &Handler{store: store, db: querier}
}

// LoginHandler redirects to Google for authentication.
// Generates a random state parameter stored in session to prevent CSRF.
func (h *Handler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sess, err := h.store.Get(r, sessionName)
	if err != nil {
		// On decode error (e.g. key rotation), start fresh.
		sess = sessions.NewSession(h.store, sessionName)
	}
	sess.Values["state"] = state
	if err := sess.Save(r, w); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, h.oauth2.AuthCodeURL(state), http.StatusFound)
}

// CallbackHandler handles the OAuth2 callback from Google.
// It validates the state parameter, verifies the ID token, upserts the user
// in the DB, stores user info in the session, and redirects to /portal.
func (h *Handler) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := h.store.Get(r, sessionName)
	if err != nil {
		http.Error(w, "bad session", http.StatusBadRequest)
		return
	}

	// Validate state to prevent CSRF.
	expectedState, ok := sess.Values["state"].(string)
	if !ok || expectedState == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != expectedState {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}
	// Clear the one-time state value immediately.
	delete(sess.Values, "state")

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	token, err := h.oauth2.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "missing id_token", http.StatusInternalServerError)
		return
	}

	verifier := h.provider.Verifier(&gooidc.Config{ClientID: h.oauth2.ClientID})
	idToken, err := verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "id_token verification failed", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "failed to parse claims", http.StatusInternalServerError)
		return
	}
	if !claims.EmailVerified {
		http.Error(w, "email not verified", http.StatusUnauthorized)
		return
	}

	user, err := h.db.UpsertUser(r.Context(), claims.Email)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	userIDStr := uuidToString(user.ID)
	sess.Values["user_id"] = userIDStr
	sess.Values["user_email"] = user.Email
	if err := sess.Save(r, w); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/portal", http.StatusFound)
}

// LogoutHandler clears the session cookie and redirects to the login page.
// The ?signed_out=1 param causes loginPageHandler to show a "signed out" flash.
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := h.store.Get(r, sessionName)
	if err == nil {
		sess.Options.MaxAge = -1
		_ = sess.Save(r, w)
	}
	http.Redirect(w, r, "/auth/login-page?signed_out=1", http.StatusFound)
}

// RequireAuth is chi middleware that checks for a valid session.
// If no valid session exists, it redirects to /auth/login.
// If valid, it injects the UserContext into the request context.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := h.store.Get(r, sessionName)
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		userIDStr, ok1 := sess.Values["user_id"].(string)
		email, ok2 := sess.Values["user_email"].(string)
		if !ok1 || !ok2 || userIDStr == "" || email == "" {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		userID, err := stringToUUID(userIDStr)
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusFound)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, &UserContext{
			UserID: userID,
			Email:  email,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// randomState generates 16 cryptographically random bytes encoded as base64url.
func randomState() (string, error) {
	b := make([]byte, stateLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// uuidToString converts a pgtype.UUID to its canonical string form.
func uuidToString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// stringToUUID parses a UUID string into a pgtype.UUID.
func stringToUUID(s string) (pgtype.UUID, error) {
	// Strip hyphens and decode the 32 hex chars.
	clean := ""
	for _, c := range s {
		if c != '-' {
			clean += string(c)
		}
	}
	if len(clean) != 32 {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID length: %d", len(clean))
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID hex: %w", err)
	}
	var u pgtype.UUID
	copy(u.Bytes[:], b)
	u.Valid = true
	return u, nil
}
