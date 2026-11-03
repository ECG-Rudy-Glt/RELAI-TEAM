package server

import (
	"context"
	"net/http"
)

func withIdentity(ctx context.Context, who string) context.Context {
	return context.WithValue(ctx, ctxKey{}, who)
}

// identity is the authenticated team member making the request.
func identity(r *http.Request) string {
	who, _ := r.Context().Value(ctxKey{}).(string)
	return who
}
