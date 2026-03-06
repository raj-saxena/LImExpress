package auth

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type contextKey int

const userContextKey contextKey = iota

// UserContext holds the authenticated user's identity from session.
type UserContext struct {
	UserID pgtype.UUID
	Email  string
}

// UserFromContext retrieves UserContext from request context.
// Returns false if no user is present in the context.
func UserFromContext(ctx context.Context) (*UserContext, bool) {
	u, ok := ctx.Value(userContextKey).(*UserContext)
	return u, ok
}
