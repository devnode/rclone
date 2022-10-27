package http2

import (
	"context"
	"net"
	"net/http"
	"sync"

	"github.com/rclone/rclone/fs"
)

type CtxKey string

var (
	CtxIsUnixSocket CtxKey = "CtxIsUnixSocket"
	CtxPublicURL    CtxKey = "CtxPublicURL"
)

func NewBaseContext(url string, useTLS bool) func(l net.Listener) context.Context {
	return func(l net.Listener) context.Context {
		ctx := context.Background()

		if l.Addr().Network() == "unix" {
			ctx = context.WithValue(ctx, CtxIsUnixSocket, true)
			return ctx
		}

		ctx = context.WithValue(ctx, CtxPublicURL, url)
		return ctx
	}
}

func IsUnixSocket(r *http.Request) bool {
	v, _ := r.Context().Value(CtxIsUnixSocket).(bool)
	return v
}

func PublicURL(r *http.Request) string {
	v, _ := r.Context().Value(CtxPublicURL).(string)
	return v
}

var onlyOnceWarningAllowOrigin sync.Once

func MiddlewareCORS(allowOrigin string) Middleware {
	onlyOnceWarningAllowOrigin.Do(func() {
		if allowOrigin == "*" {
			fs.Logf(nil, "Warning: Allow origin set to *. This can cause serious security problems.")
		}
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// skip cors for unix sockets
			if IsUnixSocket(r) {
				next.ServeHTTP(w, r)
				return
			}

			if allowOrigin != "" {
				w.Header().Add("Access-Control-Allow-Origin", allowOrigin)
			} else {
				w.Header().Add("Access-Control-Allow-Origin", PublicURL(r))
			}

			// echo back access control headers client needs
			//reqAccessHeaders := r.Header.Get("Access-Control-Request-Headers")
			w.Header().Add("Access-Control-Request-Method", "POST, OPTIONS, GET, HEAD")
			w.Header().Add("Access-Control-Allow-Headers", "authorization, Content-Type")

			next.ServeHTTP(w, r)
		})
	}
}

func MiddlewareStripPrefix(prefix string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.StripPrefix(prefix, next)
	}
}
