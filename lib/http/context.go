package http

import (
	"context"
	"net"
	"net/http"
)

type ctxKey int

const (
	ctxKeyAuth ctxKey = iota
	ctxKeyPublicURL
	ctxKeyUnixSock
	ctxKeyUser
)

// NewBaseContext initializes the context for all requests, adding info for use in middleware and handlers
func NewBaseContext(ctx context.Context, url string) func(l net.Listener) context.Context {
	return func(l net.Listener) context.Context {
		if l.Addr().Network() == "unix" {
			ctx = context.WithValue(ctx, ctxKeyUnixSock, true)
			return ctx
		}

		ctx = context.WithValue(ctx, ctxKeyPublicURL, url)
		return ctx
	}
}

// IsAuthenticated checks if this request was authenticated via a middleware
func IsAuthenticated(r *http.Request) bool {
	if v := r.Context().Value(ctxKeyAuth); v != nil {
		return true
	}
	if v := r.Context().Value(ctxKeyUser); v != nil {
		return true
	}
	return false
}

// IsUnixSocket checks if the request was received on a unix socket, used to skip auth & CORS
func IsUnixSocket(r *http.Request) bool {
	v, _ := r.Context().Value(ctxKeyUnixSock).(bool)
	return v
}

// PublicURL returns the URL defined in NewBaseContext, used for logging & CORS
func PublicURL(r *http.Request) string {
	v, _ := r.Context().Value(ctxKeyPublicURL).(string)
	return v
}

// CtxGetAuth is a wrapper over the private Auth context key
func CtxGetAuth(ctx context.Context) interface{} {
	return ctx.Value(ctxKeyAuth)
}

// CtxGetUser is a wrapper over the private User context key
func CtxGetUser(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUser).(string)
	return v, ok
}

// CtxSetUser is a test helper that injects a User value into context
func CtxSetUser(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, ctxKeyUser, value)
}
