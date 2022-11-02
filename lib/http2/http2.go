// Package http provides a registration interface for http services
package http2

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rclone/rclone/fs/config/flags"
	"github.com/rclone/rclone/fs/rc"
	"github.com/rclone/rclone/lib/http2/auth"
	"github.com/spf13/pflag"
)

// Help contains text describing the http server to add to the command
// help.
var Help = `
### Server options

Use ` + "`--addr`" + ` to specify which IP address and port the server should
listen on, eg ` + "`--addr 1.2.3.4:8000` or `--addr :8080`" + ` to listen to all
IPs.  By default it only listens on localhost.  You can use port
:0 to let the OS choose an available port.

If you set ` + "`--addr`" + ` to listen on a public or LAN accessible IP address
then using Authentication is advised - see the next section for info.

` + "`--server-read-timeout` and `--server-write-timeout`" + ` can be used to
control the timeouts on the server.  Note that this is the total time
for a transfer.

` + "`--max-header-bytes`" + ` controls the maximum number of bytes the server will
accept in the HTTP header.

` + "`--baseurl`" + ` controls the URL prefix that rclone serves from.  By default
rclone will serve from the root.  If you used ` + "`--baseurl \"/rclone\"`" + ` then
rclone would serve from a URL starting with "/rclone/".  This is
useful if you wish to proxy rclone serve.  Rclone automatically
inserts leading and trailing "/" on ` + "`--baseurl`" + `, so ` + "`--baseurl \"rclone\"`" + `,
` + "`--baseurl \"/rclone\"` and `--baseurl \"/rclone/\"`" + ` are all treated
identically.

#### TLS

By default this will serve over http.  If you want you can serve over
https.  You will need to supply the ` + "`--cert` and `--key`" + ` flags.
If you wish to do client side certificate validation then you will need to
supply ` + "`--client-ca`" + ` also.

` + "`--cert`" + ` should be a either a PEM encoded certificate or a concatenation
of that with the CA certificate.  ` + "`--key`" + ` should be the PEM encoded
private key and ` + "`--client-ca`" + ` should be the PEM encoded client
certificate authority certificate.

--min-tls-version is minimum TLS version that is acceptable. Valid
  values are "tls1.0", "tls1.1", "tls1.2" and "tls1.3" (default
  "tls1.0").
`

// Middleware function signature required by chi.Router.Use()
type Middleware func(http.Handler) http.Handler

// Config contains options for the http Server
type Config struct {
	Addrs              []string      // Port to listen on
	BaseURL            string        // prefix to strip from URLs
	ServerReadTimeout  time.Duration // Timeout for server reading data
	ServerWriteTimeout time.Duration // Timeout for server writing data
	MaxHeaderBytes     int           // Maximum size of request header
	TLSCert            string        // Path to TLS PEM key (concatenation of certificate and CA certificate)
	TLSKey             string        // Path to TLS PEM Private key
	TLSCertBody        []byte        // TLS PEM key (concatenation of certificate and CA certificate) body, ignores TLSCert
	TLSKeyBody         []byte        // TLS PEM Private key body, ignores TLSKey
	ClientCA           string        // Client certificate authority to verify clients with
	MinTLSVersion      string        // MinTLSVersion contains the minimum TLS version that is acceptable.
}

// DefaultCfg is the default values used for Config
var DefaultCfg = Config{
	Addrs:              []string{"127.0.0.1:8080"},
	ServerReadTimeout:  1 * time.Hour,
	ServerWriteTimeout: 1 * time.Hour,
	MaxHeaderBytes:     4096,
	MinTLSVersion:      "tls1.0",
}

// Server interface of http server
type Server interface {
	Router() chi.Router
	Route(pattern string, fn func(r chi.Router)) chi.Router
	Mount(pattern string, h http.Handler)
	Serve()
	Shutdown() error
	URLs() []string
	Wait()
}

type instance struct {
	url        string
	listener   net.Listener
	httpServer *http.Server
}

func (s instance) serve(wg *sync.WaitGroup) {
	defer wg.Done()
	var err error
	if s.httpServer.TLSConfig != nil {
		err = s.httpServer.ServeTLS(s.listener, "", "")
	} else {
		err = s.httpServer.Serve(s.listener)
	}
	if err != http.ErrServerClosed && err != nil {
		log.Fatalf(err.Error())
	}
}

type server struct {
	cfg       Config
	mux       chi.Router
	wg        sync.WaitGroup
	auth      auth.Options
	tlsConfig *tls.Config
	instances []instance
}

type Option func(*server)

func WithAuth(auth auth.Options) Option {
	return func(s *server) {
		s.auth = auth
	}
}

func WithConfig(cfg Config) Option {
	return func(s *server) {
		s.cfg = cfg
	}
}

func WithRC(rco *rc.Options) Option {
	return func(s *server) {
		httpOpts := rco.HTTPOptions

		s.cfg.Addrs = []string{httpOpts.ListenAddr}
		s.cfg.BaseURL = httpOpts.BaseURL
		s.cfg.ServerReadTimeout = httpOpts.ServerReadTimeout
		s.cfg.ServerWriteTimeout = httpOpts.ServerWriteTimeout
		s.cfg.MaxHeaderBytes = httpOpts.MaxHeaderBytes
		s.cfg.TLSCert = httpOpts.SslCert
		s.cfg.TLSKey = httpOpts.SslKey
		s.cfg.ClientCA = httpOpts.ClientCA
		s.cfg.MinTLSVersion = httpOpts.MinTLSVersion

		s.auth.HtPasswd = httpOpts.HtPasswd
		s.auth.Realm = httpOpts.Realm
		s.auth.BasicUser = httpOpts.BasicUser
		s.auth.BasicPass = httpOpts.BasicPass
		// TODO: fix this type wrap
		s.auth.CustomAuthFn = auth.CustomAuthFn(httpOpts.Auth)
	}
}

// NewServer instantiates a new http server using provided listeners and options
// This function is provided if the default http server does not meet a services requirements and should not generally be used
// A http server can listen using multiple listeners. For example, a listener for port 80, and a listener for port 443.
// tlsListeners are ignored if opt.TLSKey is not provided
func NewServer(ctx context.Context, options ...Option) (*server, error) {
	s := &server{
		mux: chi.NewRouter(),
		cfg: DefaultCfg,
	}

	for _, oo := range options {
		oo(s)
	}

	// Build base router
	s.mux.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})
	s.mux.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})

	// Ignore passing "/" for BaseURL
	s.cfg.BaseURL = strings.Trim(s.cfg.BaseURL, "/")
	if s.cfg.BaseURL != "" {
		s.cfg.BaseURL = "/" + s.cfg.BaseURL
		s.mux.Use(MiddlewareStripPrefix(s.cfg.BaseURL))
	}

	s.initAuth()

	err := s.initTLS()
	if err != nil {
		return nil, fmt.Errorf("init tls: %w", err)
	}

	for _, addr := range s.cfg.Addrs {
		var url string
		var network = "tcp"
		var tlsCfg *tls.Config

		if strings.HasPrefix(addr, "unix://") || filepath.IsAbs(addr) {
			network = "unix"
			addr = strings.TrimPrefix(addr, "unix://")
			url = addr

		} else if strings.HasPrefix(addr, "tls://") || (len(s.cfg.Addrs) == 1 && s.tlsConfig != nil) {
			tlsCfg = s.tlsConfig
			addr = strings.TrimPrefix(addr, "tls://")
		}

		l, err := net.Listen(network, addr)
		if err != nil {
			return nil, fmt.Errorf("failed to start listener: %s", err)
		}

		if network == "tcp" {
			var secure string
			if tlsCfg != nil {
				secure = "s"
			}
			url = fmt.Sprintf("http%s://%s%s/", secure, l.Addr().String(), s.cfg.BaseURL)
		}

		ii := instance{
			url:      url,
			listener: l,
			httpServer: &http.Server{
				Handler:           s.mux,
				ReadTimeout:       s.cfg.ServerReadTimeout,
				WriteTimeout:      s.cfg.ServerWriteTimeout,
				MaxHeaderBytes:    s.cfg.MaxHeaderBytes,
				ReadHeaderTimeout: 10 * time.Second, // time to send the headers
				IdleTimeout:       60 * time.Second, // time to keep idle connections open
				TLSConfig:         tlsCfg,
				BaseContext:       NewBaseContext(ctx, url),
			},
		}

		s.instances = append(s.instances, ii)
	}

	return s, nil
}

func (s *server) initAuth() {
	if s.auth.CustomAuthFn != nil {
		s.mux.Use(MiddlewareAuthCustom(s.auth.CustomAuthFn, s.auth.Realm))
		return
	}

	if s.auth.HtPasswd != "" {
		s.mux.Use(MiddlewareAuthHtpasswd(s.auth.HtPasswd, s.auth.Realm))
		return
	}

	if s.auth.BasicUser != "" {
		s.mux.Use(MiddlewareAuthBasic(s.auth.BasicUser, s.auth.BasicPass, s.auth.Realm, s.auth.Salt))
		return
	}
}

func (s *server) initTLS() error {
	if s.cfg.TLSKey == "" && len(s.cfg.TLSKeyBody) == 0 {
		return nil
	}

	if (len(s.cfg.TLSCertBody) > 0) != (len(s.cfg.TLSKeyBody) > 0) {
		return fmt.Errorf("need both TLSCertBody and TLSKeyBody to use TLS")
	}

	if (s.cfg.TLSCert != "") != (s.cfg.TLSKey != "") {
		return fmt.Errorf("need both --cert and --key to use TLS")
	}

	var cert tls.Certificate
	var err error
	if len(s.cfg.TLSCertBody) > 0 {
		cert, err = tls.X509KeyPair(s.cfg.TLSCertBody, s.cfg.TLSKeyBody)
	} else {
		cert, err = tls.LoadX509KeyPair(s.cfg.TLSCert, s.cfg.TLSKey)
	}
	if err != nil {
		return fmt.Errorf("failed to load x509 keypair: %w", err)
	}

	var minTLSVersion uint16
	switch s.cfg.MinTLSVersion {
	case "tls1.0":
		minTLSVersion = tls.VersionTLS10
	case "tls1.1":
		minTLSVersion = tls.VersionTLS11
	case "tls1.2":
		minTLSVersion = tls.VersionTLS12
	case "tls1.3":
		minTLSVersion = tls.VersionTLS13
	default:
		return fmt.Errorf("invalid value for --min-tls-version: %s", s.cfg.MinTLSVersion)
	}

	s.tlsConfig = &tls.Config{
		MinVersion:   minTLSVersion,
		Certificates: []tls.Certificate{cert},
	}

	if s.cfg.ClientCA != "" {
		// if !useTLS {
		// 	err := errors.New("can't use --client-ca without --cert and --key")
		// 	log.Fatalf(err.Error())
		// }
		certpool := x509.NewCertPool()
		pem, err := os.ReadFile(s.cfg.ClientCA)
		if err != nil {
			return fmt.Errorf("failed to read client certificate authority: %w", err)
		}

		if !certpool.AppendCertsFromPEM(pem) {
			return errors.New("unable to parse client certificate authority")
		}

		s.tlsConfig.ClientCAs = certpool
		s.tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return nil
}

func (s *server) Serve() {
	s.wg.Add(len(s.instances))
	for _, ii := range s.instances {
		log.Printf("listening on %s", ii.url)
		go ii.serve(&s.wg)
	}
}

// Wait blocks while the server is serving requests
func (s *server) Wait() {
	s.wg.Wait()
}

// Router returns the server base router
func (s *server) Router() chi.Router {
	return s.mux
}

// Route mounts a sub-Router along a `pattern` string.
func (s *server) Route(pattern string, fn func(r chi.Router)) chi.Router {
	return s.mux.Route(pattern, fn)
}

// Mount attaches another http.Handler along ./pattern/*
func (s *server) Mount(pattern string, h http.Handler) {
	s.mux.Mount(pattern, h)
}

// Shutdown gracefully shuts down the server
func (s *server) Shutdown() error {
	ctx := context.Background()
	for _, ii := range s.instances {
		if err := ii.httpServer.Shutdown(ctx); err != nil {
			log.Printf("error shutting down server: %s", err)
			continue
		}
	}
	s.wg.Wait()
	return nil
}

func (s *server) URLs() []string {
	var out []string
	for _, ii := range s.instances {
		if ii.listener.Addr().Network() == "unix" {
			continue
		}
		out = append(out, ii.url)
	}
	return out
}

//---- Command line flags ----

// AddFlagsPrefix adds flags for the httplib
func AddFlagsPrefix(flagSet *pflag.FlagSet, prefix string, cfg *Config) {
	flags.StringArrayVarP(flagSet, &cfg.Addrs, prefix+"addr", "", cfg.Addrs, "IPaddress:Port or :Port to bind server to")
	flags.DurationVarP(flagSet, &cfg.ServerReadTimeout, prefix+"server-read-timeout", "", cfg.ServerReadTimeout, "Timeout for server reading data")
	flags.DurationVarP(flagSet, &cfg.ServerWriteTimeout, prefix+"server-write-timeout", "", cfg.ServerWriteTimeout, "Timeout for server writing data")
	flags.IntVarP(flagSet, &cfg.MaxHeaderBytes, prefix+"max-header-bytes", "", cfg.MaxHeaderBytes, "Maximum size of request header")
	flags.StringVarP(flagSet, &cfg.TLSCert, prefix+"cert", "", cfg.TLSCert, "TLS PEM key (concatenation of certificate and CA certificate)")
	flags.StringVarP(flagSet, &cfg.TLSKey, prefix+"key", "", cfg.TLSKey, "TLS PEM Private key")
	flags.StringVarP(flagSet, &cfg.ClientCA, prefix+"client-ca", "", cfg.ClientCA, "Client certificate authority to verify clients with")
	flags.StringVarP(flagSet, &cfg.BaseURL, prefix+"baseurl", "", cfg.BaseURL, "Prefix for URLs - leave blank for root")
	flags.StringVarP(flagSet, &cfg.MinTLSVersion, prefix+"min-tls-version", "", cfg.MinTLSVersion, "Minimum TLS version that is acceptable")
}
