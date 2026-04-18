package main

import "context"

// traceIDContextKey is the unexported key type used to store the ReAct trace ID
// in a context.  Using a private type prevents collisions with keys from other
// packages.
type traceIDContextKey struct{}

// withReActTraceID returns a copy of ctx with traceID stored under the
// package-private key.  Pass this enriched context down the call chain so that
// proposal handlers can include the trace ID in their audit log payloads.
func withReActTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDContextKey{}, traceID)
}

// reActTraceIDFromContext extracts the trace ID stored by withReActTraceID.
// Returns an empty string when no trace ID is present.
func reActTraceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(traceIDContextKey{}).(string)
	return id
}
