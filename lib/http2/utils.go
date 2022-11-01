package http2

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"sync"

	goauth "github.com/abbot/go-http-auth"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/http2/auth"
)

type CtxKey string

var (
	ContextAuthKey      CtxKey = "ContextAuthKey"
	ContextIsAuthKey    CtxKey = "ContextIsAuthKey"
	ContextUnixSockKey  CtxKey = "ContextUnixSockKey"
	ContentPublicURLKey CtxKey = "ContentPublicURLKey"
	ContextUserKey      CtxKey = "ContextUserKey"
)

func NewBaseContext(ctx context.Context, url string) func(l net.Listener) context.Context {
	return func(l net.Listener) context.Context {
		if l.Addr().Network() == "unix" {
			ctx = context.WithValue(ctx, ContextUnixSockKey, true)
			return ctx
		}

		ctx = context.WithValue(ctx, ContentPublicURLKey, url)
		return ctx
	}
}

func IsAuthenticated(r *http.Request) bool {
	if v := r.Context().Value(ContextAuthKey); v != nil {
		return true
	}
	if v := r.Context().Value(ContextAuthKey); v != nil {
		return true
	}
	return false
}

func IsUnixSocket(r *http.Request) bool {
	v, _ := r.Context().Value(ContextUnixSockKey).(bool)
	return v
}

func PublicURL(r *http.Request) string {
	v, _ := r.Context().Value(ContentPublicURLKey).(string)
	return v
}

// parseAuthorization parses the Authorization header into user, pass
// it returns a boolean as to whether the parse was successful
func parseAuthorization(r *http.Request) (user, pass string, ok bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		s := strings.SplitN(authHeader, " ", 2)
		if len(s) == 2 && s[0] == "Basic" {
			b, err := base64.StdEncoding.DecodeString(s[1])
			if err == nil {
				parts := strings.SplitN(string(b), ":", 2)
				user = parts[0]
				if len(parts) > 1 {
					pass = parts[1]
					ok = true
				}
			}
		}
	}
	return
}

type LoggedBasicAuth struct {
	goauth.BasicAuth
}

// CheckAuth extends BasicAuth.CheckAuth to emit a log entry for unauthorised requests
func (a *LoggedBasicAuth) CheckAuth(r *http.Request) string {
	username := a.BasicAuth.CheckAuth(r)
	if username == "" {
		user, _, _ := parseAuthorization(r)
		fs.Infof(r.URL.Path, "%s: Unauthorized request from %s", r.RemoteAddr, user)
	}
	return username
}

// NewLoggedBasicAuthenticator instantiates a new instance of LoggedBasicAuthenticator
func NewLoggedBasicAuthenticator(realm string, secrets goauth.SecretProvider) *LoggedBasicAuth {
	return &LoggedBasicAuth{BasicAuth: goauth.BasicAuth{Realm: realm, Secrets: secrets}}
}

// Helper to generate required interface for middleware
func basicAuth(authenticator *LoggedBasicAuth) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username := authenticator.CheckAuth(r)
			if username == "" {
				authenticator.RequireAuth(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), ContextUserKey, username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// MiddlewareAuthHtpasswd instantiates middleware that authenticates against the passed htpasswd file
func MiddlewareAuthHtpasswd(path, realm string) Middleware {
	fs.Infof(nil, "Using %q as htpasswd storage", path)
	secretProvider := goauth.HtpasswdFileProvider(path)
	authenticator := NewLoggedBasicAuthenticator(realm, secretProvider)
	return basicAuth(authenticator)
}

// MiddlewareAuthBasic instantiates middleware that authenticates for a single user
func MiddlewareAuthBasic(user, pass, realm, salt string) Middleware {
	fs.Infof(nil, "Using --user %s --pass XXXX as authenticated user", user)
	pass = string(goauth.MD5Crypt([]byte(pass), []byte(salt), []byte("$1$")))
	secretProvider := func(u, r string) string {
		if user == u {
			return pass
		}
		return ""
	}
	authenticator := NewLoggedBasicAuthenticator(realm, secretProvider)
	return basicAuth(authenticator)
}

// MiddlewareAuthCustom instantiates middleware that authenticates using a custom function
func MiddlewareAuthCustom(fn auth.CustomAuthFn, realm string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, pass, ok := parseAuthorization(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			value, err := fn(user, pass)
			if err != nil {
				fs.Infof(r.URL.Path, "%s: Auth failed from %s: %v", r.RemoteAddr, user, err)
				goauth.NewBasicAuthenticator(realm, func(user, realm string) string { return "" }).RequireAuth(w, r) //Reuse BasicAuth error reporting
				return
			}

			if value != nil {
				r = r.WithContext(context.WithValue(r.Context(), ContextAuthKey, value))
			}

			next.ServeHTTP(w, r)
		})
	}
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
