package http

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var _testCORSHeaderKeys = []string{
	"Access-Control-Allow-Origin",
	"Access-Control-Request-Method",
	"Access-Control-Allow-Headers",
}

func testEchoHandler(data []byte) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	})
}

func testNewHTTPClientUnix(path string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", path)
			},
		},
	}
}

func testGetServerURL(t *testing.T, s Server) string {
	urls := s.URLs()
	require.GreaterOrEqual(t, len(urls), 1, "server should return at least one url")
	return urls[0]
}

func testExpectRespBody(t *testing.T, resp *http.Response, expected []byte) {
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, expected, body)
}

func TestNewServerUnix(t *testing.T) {
	ctx := context.Background()

	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "rclone.sock")

	cfg := DefaultHTTPCfg
	cfg.ListenAddr = []string{path}

	auth := &AuthConfig{
		BasicUser: "test",
		BasicPass: "test",
	}

	s, err := NewServer(ctx, WithConfig(cfg), WithAuth(auth))
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Shutdown())
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, "shutdown should remove socket")
	}()

	require.Empty(t, s.URLs(), "unix socket should not appear in URLs")

	s.Router().Use(MiddlewareCORS(""))

	expected := []byte("hello world")
	s.Router().Mount("/", testEchoHandler(expected))
	s.Serve()

	client := testNewHTTPClientUnix(path)
	req, err := http.NewRequest("GET", "http://unix", nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)

	testExpectRespBody(t, resp, expected)

	require.Equal(t, http.StatusOK, resp.StatusCode, "unix sockets should ignore auth")

	for _, key := range _testCORSHeaderKeys {
		require.NotContains(t, resp.Header, key, "unix sockets should not be sent CORS headers")
	}
}

func TestNewServerHTTP(t *testing.T) {
	ctx := context.Background()

	cfg := DefaultHTTPCfg
	cfg.ListenAddr = []string{"127.0.0.1:0"}

	auth := &AuthConfig{
		BasicUser: "test",
		BasicPass: "test",
	}

	s, err := NewServer(ctx, WithConfig(cfg), WithAuth(auth))
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.Shutdown())
	}()

	url := testGetServerURL(t, s)

	s.Router().Use(MiddlewareCORS(""))

	expected := []byte("hello world")
	s.Router().Mount("/", testEchoHandler(expected))
	s.Serve()

	t.Run("StatusUnauthorized", func(t *testing.T) {
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, "no basic auth creds should return unauthorized")
	})

	t.Run("StatusOK", func(t *testing.T) {
		client := &http.Client{}
		req, err := http.NewRequest("GET", url, nil)
		require.NoError(t, err)

		req.SetBasicAuth(auth.BasicUser, auth.BasicPass)

		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode, "using basic auth creds should return ok")

		testExpectRespBody(t, resp, expected)
	})
}
