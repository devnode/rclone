// Package rc implements a remote control server and registry for rclone
//
// To register your internal calls, call rc.Add(path, function).  Your
// function should take ane return a Param.  It can also return an
// error.  Use rc.NewError to wrap an existing error along with an
// http response type if another response other than 500 internal
// error is required on error.
package rc

import (
	"encoding/json"
	"io"
	_ "net/http/pprof" // install the pprof http handlers
	"time"

	"github.com/rclone/rclone/cmd/serve/httplib"
)

// Options contains options for the remote control server
type Options struct {
	HTTPOptions              httplib.Options `prefix:"rc-"`
	Enabled                  bool            `flag:"rc"`                         // set to enable the server
	Serve                    bool            `flag:"rc-serve"`                   // set to serve files from remotes
	Files                    string          `flag:"rc-files"`                   // set to enable serving files locally
	NoAuth                   bool            `flag:"rc-no-auth"`                 // set to disable auth checks on AuthRequired methods
	WebUI                    bool            `flag:"rc-web-gui"`                 // set to launch the web ui
	WebGUIUpdate             bool            `flag:"rc-web-gui-update"`          // set to check new update
	WebGUIForceUpdate        bool            `flag:"rc-web-gui-force-update"`    // set to force download new update
	WebGUINoOpenBrowser      bool            `flag:"rc-web-gui-no-open-browser"` // set to disable auto opening browser
	WebGUIFetchURL           string          `flag:"rc-web-fetch-url"`           // set the default url for fetching webgui
	AccessControlAllowOrigin string          `flag:"rc-allow-origin"`            // set the access control for CORS configuration
	EnableMetrics            bool            `flag:"rc-enable-metrics"`          // set to disable prometheus metrics on /metrics
	JobExpireDuration        time.Duration   `flag:"rc-job-expire-duration"`
	JobExpireInterval        time.Duration   `flag:"rc-job-expire-interval"`
}

// DefaultOpt is the default values used for Options
var DefaultOpt = Options{
	HTTPOptions:       httplib.DefaultOpt,
	Enabled:           false,
	JobExpireDuration: 60 * time.Second,
	JobExpireInterval: 10 * time.Second,
}

func init() {
	DefaultOpt.HTTPOptions.ListenAddr = "localhost:5572"
}

// WriteJSON writes JSON in out to w
func WriteJSON(w io.Writer, out Params) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	return enc.Encode(out)
}
