package middleware

import "context"

// WithTestContext injects a KeyAuthContext into ctx using the same internal key
// that VirtualKeyAuth uses. This is intended only for use in tests — production
// code must go through VirtualKeyAuth.
func WithTestContext(ctx context.Context, kac *KeyAuthContext) context.Context {
	return context.WithValue(ctx, keyAuthContextKey, kac)
}
