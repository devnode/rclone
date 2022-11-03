package restic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/rclone/rclone/cmd"
	"github.com/rclone/rclone/fs/config/configfile"
	"github.com/stretchr/testify/require"
)

// createOverwriteDeleteSeq returns a sequence which will create a new file at
// path, and then try to overwrite and delete it.
func createOverwriteDeleteSeq(t testing.TB, path string) []TestRequest {
	// add a file, try to overwrite and delete it
	req := []TestRequest{
		{
			req:  newRequest(t, "GET", path, nil),
			want: []wantFunc{wantCode(http.StatusNotFound)},
		},
		{
			req:  newRequest(t, "POST", path, strings.NewReader("foobar test config")),
			want: []wantFunc{wantCode(http.StatusOK)},
		},
		{
			req: newRequest(t, "GET", path, nil),
			want: []wantFunc{
				wantCode(http.StatusOK),
				wantBody("foobar test config"),
			},
		},
		{
			req:  newRequest(t, "POST", path, strings.NewReader("other config")),
			want: []wantFunc{wantCode(http.StatusForbidden)},
		},
		{
			req: newRequest(t, "GET", path, nil),
			want: []wantFunc{
				wantCode(http.StatusOK),
				wantBody("foobar test config"),
			},
		},
		{
			req:  newRequest(t, "DELETE", path, nil),
			want: []wantFunc{wantCode(http.StatusForbidden)},
		},
		{
			req: newRequest(t, "GET", path, nil),
			want: []wantFunc{
				wantCode(http.StatusOK),
				wantBody("foobar test config"),
			},
		},
	}
	return req
}

// TestResticHandler runs tests on the restic handler code, especially in append-only mode.
func TestResticHandler(t *testing.T) {
	configfile.Install()
	buf := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, buf)
	require.NoError(t, err)
	randomID := hex.EncodeToString(buf)

	var tests = []struct {
		name string
		seq  []TestRequest
	}{
		{
			name: "CreateOverwriteDeleteSeq/Config",
			seq:  createOverwriteDeleteSeq(t, "/config"),
		},
		{
			name: "CreateOverwriteDeleteSeq/Data/RandomID",
			seq:  createOverwriteDeleteSeq(t, "/data/"+randomID),
		},
		{
			// ensure we can add and remove lock files
			name: "AddRemoveLockFiles",
			seq: []TestRequest{
				{
					req:  newRequest(t, "GET", "/locks/"+randomID, nil),
					want: []wantFunc{wantCode(http.StatusNotFound)},
				},
				{
					req:  newRequest(t, "POST", "/locks/"+randomID, strings.NewReader("lock file")),
					want: []wantFunc{wantCode(http.StatusOK)},
				},
				{
					req: newRequest(t, "GET", "/locks/"+randomID, nil),
					want: []wantFunc{
						wantCode(http.StatusOK),
						wantBody("lock file"),
					},
				},
				{
					req:  newRequest(t, "POST", "/locks/"+randomID, strings.NewReader("other lock file")),
					want: []wantFunc{wantCode(http.StatusForbidden)},
				},
				{
					req:  newRequest(t, "DELETE", "/locks/"+randomID, nil),
					want: []wantFunc{wantCode(http.StatusOK)},
				},
				{
					req:  newRequest(t, "GET", "/locks/"+randomID, nil),
					want: []wantFunc{wantCode(http.StatusNotFound)},
				},
			},
		},
	}

	// setup rclone with a local backend in a temporary directory
	tempdir := t.TempDir()

	// make a new file system in the temp dir
	f := cmd.NewFsSrc([]string{tempdir})

	opt := DefaultOpt
	opt.AppendOnly = true
	s, err := newRestic(context.Background(), f, opt)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, s.server.Shutdown())
	}()

	// create the repo
	checkRequest(t, s.ServeHTTP,
		newRequest(t, "POST", "/?create=true", nil),
		[]wantFunc{wantCode(http.StatusOK)})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for i, seq := range test.seq {
				t.Logf("request %v: %v %v", i, seq.req.Method, seq.req.URL.Path)
				checkRequest(t, s.ServeHTTP, seq.req, seq.want)
			}
		})
	}
}
