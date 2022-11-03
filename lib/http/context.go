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

func IsAuthenticated(r *http.Request) bool {
	if v := r.Context().Value(ctxKeyAuth); v != nil {
		return true
	}
	if v := r.Context().Value(ctxKeyUser); v != nil {
		return true
	}
	return false
}

func IsUnixSocket(r *http.Request) bool {
	v, _ := r.Context().Value(ctxKeyUnixSock).(bool)
	return v
}

func PublicURL(r *http.Request) string {
	v, _ := r.Context().Value(ctxKeyPublicURL).(string)
	return v
}

func CtxGetAuth(ctx context.Context) interface{} {
	return ctx.Value(ctxKeyAuth)
}

func CtxGetUser(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyUser).(string)
	return v, ok
}

func CtxSetUser(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, ctxKeyUser, value)
}
