package auth

import "context"

// ContextWithOrg stores OrgContext in ctx and returns the derived context.
func ContextWithOrg(ctx context.Context, o *OrgContext) context.Context {
	return context.WithValue(ctx, orgContextKey, o)
}
