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

#### TLS/TLS

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

// Options contains options for the http Server
type Options struct {
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

// DefaultOpt is the default values used for Options
var DefaultOpt = Options{
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
	mux       chi.Router
	wg        sync.WaitGroup
	tlsConfig *tls.Config
	instances []instance
	useTLS    bool
	opt       Options
}

func UseTLS(opt Options) bool {
	return opt.TLSKey != "" || len(opt.TLSKeyBody) > 0
}

// NewServer instantiates a new http server using provided listeners and options
// This function is provided if the default http server does not meet a services requirements and should not generally be used
// A http server can listen using multiple listeners. For example, a listener for port 80, and a listener for port 443.
// tlsListeners are ignored if opt.TLSKey is not provided
func NewServer(opt Options) (Server, error) {
	s := &server{
		mux:    chi.NewRouter(),
		useTLS: UseTLS(opt),
		opt:    opt,
	}

	if (len(opt.TLSCertBody) > 0) != (len(opt.TLSKeyBody) > 0) {
		log.Fatalf("need both TLSCertBody and TLSKeyBody to use TLS")
	}

	if (opt.TLSCert != "") != (opt.TLSKey != "") {
		log.Fatalf("need both --cert and --key to use TLS")
	}

	if s.useTLS {
		var cert tls.Certificate
		var err error
		if len(opt.TLSCertBody) > 0 {
			cert, err = tls.X509KeyPair(opt.TLSCertBody, opt.TLSKeyBody)
		} else {
			cert, err = tls.LoadX509KeyPair(opt.TLSCert, opt.TLSKey)
		}
		if err != nil {
			log.Fatal(err)
		}
		var minTLSVersion uint16
		switch opt.MinTLSVersion {
		case "tls1.0":
			minTLSVersion = tls.VersionTLS10
		case "tls1.1":
			minTLSVersion = tls.VersionTLS11
		case "tls1.2":
			minTLSVersion = tls.VersionTLS12
		case "tls1.3":
			minTLSVersion = tls.VersionTLS13
		default:
			log.Fatalf("Invalid value for --min-tls-version: %s", opt.MinTLSVersion)
		}
		s.tlsConfig = &tls.Config{
			MinVersion:   minTLSVersion,
			Certificates: []tls.Certificate{cert},
		}
	}

	if opt.ClientCA != "" {
		if !s.useTLS {
			err := errors.New("can't use --client-ca without --cert and --key")
			log.Fatalf(err.Error())
			return nil, err
		}
		certpool := x509.NewCertPool()
		pem, err := os.ReadFile(opt.ClientCA)
		if err != nil {
			log.Fatalf("Failed to read client certificate authority: %v", err)
			return nil, err
		}
		if !certpool.AppendCertsFromPEM(pem) {
			err := errors.New("can't parse client certificate authority")
			log.Fatalf(err.Error())
			return nil, err
		}
		s.tlsConfig.ClientCAs = certpool
		s.tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	// Ignore passing "/" for BaseURL
	opt.BaseURL = strings.Trim(opt.BaseURL, "/")
	if opt.BaseURL != "" {
		opt.BaseURL = "/" + opt.BaseURL
	}

	// Build base router
	s.mux.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	})
	s.mux.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	})

	if opt.BaseURL != "" {
		s.mux.Use(MiddlewareStripPrefix(opt.BaseURL))
	}

	for _, addr := range opt.Addrs {
		var url string
		var network = "tcp"
		var tlsCfg *tls.Config

		if strings.HasPrefix(addr, "unix://") || filepath.IsAbs(addr) {
			network = "unix"
			addr = strings.TrimPrefix(addr, "unix://")
			url = addr

		} else if strings.HasPrefix(addr, "tls://") || len(opt.Addrs) == 1 && s.useTLS {
			tlsCfg = s.tlsConfig
			addr = strings.TrimPrefix(addr, "tls://")
		}

		l, err := net.Listen(network, addr)
		if err != nil {
			log.Fatalf("failed to setup listener for: %s", err)
		}

		if network == "tcp" {
			var secure string
			if tlsCfg != nil {
				secure = "s"
			}
			url = fmt.Sprintf("http%s://%s%s/", secure, l.Addr().String(), opt.BaseURL)
		}

		ii := instance{
			url:      url,
			listener: l,
			httpServer: &http.Server{
				Handler:           s.mux,
				ReadTimeout:       opt.ServerReadTimeout,
				WriteTimeout:      opt.ServerWriteTimeout,
				MaxHeaderBytes:    opt.MaxHeaderBytes,
				ReadHeaderTimeout: 10 * time.Second, // time to send the headers
				IdleTimeout:       60 * time.Second, // time to keep idle connections open
				TLSConfig:         tlsCfg,
				BaseContext:       NewBaseContext(opt.BaseURL, tlsCfg != nil),
			},
		}

		s.instances = append(s.instances, ii)
	}

	return s, nil
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

//---- Command line flags ----

// AddFlagsPrefix adds flags for the httplib
func AddFlagsPrefix(flagSet *pflag.FlagSet, prefix string, Opt *Options) {
	flags.StringArrayVarP(flagSet, &Opt.Addrs, prefix+"addr", "", Opt.Addrs, "IPaddress:Port or :Port to bind server to")
	flags.DurationVarP(flagSet, &Opt.ServerReadTimeout, prefix+"server-read-timeout", "", Opt.ServerReadTimeout, "Timeout for server reading data")
	flags.DurationVarP(flagSet, &Opt.ServerWriteTimeout, prefix+"server-write-timeout", "", Opt.ServerWriteTimeout, "Timeout for server writing data")
	flags.IntVarP(flagSet, &Opt.MaxHeaderBytes, prefix+"max-header-bytes", "", Opt.MaxHeaderBytes, "Maximum size of request header")
	flags.StringVarP(flagSet, &Opt.TLSCert, prefix+"cert", "", Opt.TLSCert, "TLS PEM key (concatenation of certificate and CA certificate)")
	flags.StringVarP(flagSet, &Opt.TLSKey, prefix+"key", "", Opt.TLSKey, "TLS PEM Private key")
	flags.StringVarP(flagSet, &Opt.ClientCA, prefix+"client-ca", "", Opt.ClientCA, "Client certificate authority to verify clients with")
	flags.StringVarP(flagSet, &Opt.BaseURL, prefix+"baseurl", "", Opt.BaseURL, "Prefix for URLs - leave blank for root")
	flags.StringVarP(flagSet, &Opt.MinTLSVersion, prefix+"min-tls-version", "", Opt.MinTLSVersion, "Minimum TLS version that is acceptable")

}
