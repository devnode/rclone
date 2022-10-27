package rcd2

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/rc"
	"github.com/rclone/rclone/fs/rc/jobs"
)

func NewListener(in string) (net.Listener, error) {
	if filepath.IsAbs(in) {
		return net.Listen("unix", in)
	}
	return net.Listen("tcp", in)
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		path := strings.TrimPrefix(r.URL.Path, "/")
		contentType := r.Header.Get("Content-Type")

		values := r.URL.Query()
		if contentType == "application/x-www-form-urlencoded" {
			// Parse the POST and URL parameters into r.Form, for others r.Form will be empty value
			err := r.ParseForm()
			if err != nil {
				writeError(path, nil, w, fmt.Errorf("failed to parse form/URL parameters: %w", err), http.StatusBadRequest)
				return
			}
			values = r.Form
		}

		// Read the POST and URL parameters into in
		in := make(rc.Params)
		for k, vs := range values {
			if len(vs) > 0 {
				in[k] = vs[len(vs)-1]
			}
		}

		// Parse a JSON blob from the input
		if contentType == "application/json" {
			err := json.NewDecoder(r.Body).Decode(&in)
			if err != nil {
				writeError(path, in, w, fmt.Errorf("failed to read input JSON: %w", err), http.StatusBadRequest)
				return
			}
		}

		// Find the call
		call := rc.Calls.Get(path)
		if call == nil {
			writeError(path, in, w, fmt.Errorf("couldn't find method %q", path), http.StatusNotFound)
			return
		}

		// TODO: auth check

		inOrig := in.Copy()

		if call.NeedsRequest {
			// Add the request to RC
			in["_request"] = r
		}

		if call.NeedsResponse {
			in["_response"] = w
		}

		fs.Debugf(nil, "rc: %q: with parameters %+v", path, in)
		job, out, err := jobs.NewJob(ctx, call.Fn, in)
		if job != nil {
			w.Header().Add("x-rclone-jobid", fmt.Sprintf("%d", job.ID))
		}
		if err != nil {
			writeError(path, inOrig, w, err, http.StatusInternalServerError)
			return
		}
		if out == nil {
			out = make(rc.Params)
		}

		fs.Debugf(nil, "rc: %q: reply %+v: %v", path, out, err)
		err = rc.WriteJSON(w, out)
		if err != nil {
			// can't return the error at this point - but have a go anyway
			writeError(path, inOrig, w, err, http.StatusInternalServerError)
			fs.Errorf(nil, "rc: handlePost: failed to write JSON output: %v", err)
		}
	})
}

// writeError writes a formatted error to the output
func writeError(path string, in rc.Params, w http.ResponseWriter, err error, status int) {
	fs.Errorf(nil, "rc: %q: error: %v", path, err)
	params, status := rc.Error(path, in, err, status)
	w.WriteHeader(status)
	err = rc.WriteJSON(w, params)
	if err != nil {
		// can't return the error at this point
		fs.Errorf(nil, "rc: writeError: failed to write JSON output from %#v: %v", in, err)
	}
}
