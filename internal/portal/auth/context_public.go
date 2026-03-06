package auth

import "context"

// ContextWithUser stores UserContext in ctx and returns the derived context.
func ContextWithUser(ctx context.Context, u *UserContext) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}
