package restic

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rclone/rclone/cmd"
	libhttp "github.com/rclone/rclone/lib/http"
	"github.com/stretchr/testify/require"
)

// newAuthenticatedRequest returns a new HTTP request with the given params.
func newAuthenticatedRequest(t testing.TB, method, path string, body io.Reader) *http.Request {
	req := newRequest(t, method, path, body)
	req = req.WithContext(libhttp.CtxSetUser(req.Context(), "test"))
	req.Header.Add("Accept", resticAPIV2)
	return req
}

// TestResticPrivateRepositories runs tests on the restic handler code for private repositories
func TestResticPrivateRepositories(t *testing.T) {
	buf := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, buf)
	require.NoError(t, err)

	// setup rclone with a local backend in a temporary directory
	tempdir := t.TempDir()

	// globally set private-repos mode & test user
	opt := DefaultOpt
	opt.PrivateRepos = true
	opt.Auth.BasicUser = "test"
	opt.Auth.BasicPass = "password"

	// make a new file system in the temp dir
	f := cmd.NewFsSrc([]string{tempdir})
	r, err := newRestic(context.Background(), f, opt)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, r.server.Shutdown())
	}()

	// Requesting /test/ should allow access
	reqs := []*http.Request{
		newAuthenticatedRequest(t, "POST", "/test/?create=true", nil),
		newAuthenticatedRequest(t, "POST", "/test/config", strings.NewReader("foobar test config")),
		newAuthenticatedRequest(t, "GET", "/test/config", nil),
	}
	for _, req := range reqs {
		checkRequest(t, r.ServeHTTP, req, []wantFunc{wantCode(http.StatusOK)})
	}

	// Requesting everything else should raise forbidden errors
	reqs = []*http.Request{
		newAuthenticatedRequest(t, "GET", "/", nil),
		newAuthenticatedRequest(t, "POST", "/other_user", nil),
		newAuthenticatedRequest(t, "GET", "/other_user/config", nil),
	}
	for _, req := range reqs {
		checkRequest(t, r.ServeHTTP, req, []wantFunc{wantCode(http.StatusForbidden)})
	}

}
