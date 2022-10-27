// Package rcd provides the rcd command.
package rcd2

import (
	"log"
	"net"
	"strings"
	"sync"

	sysdnotify "github.com/iguanesolutions/go-systemd/v5/notify"
	"github.com/rclone/rclone/cmd"
	"github.com/rclone/rclone/fs/config/flags"
	"github.com/rclone/rclone/lib/atexit"
	libhttp "github.com/rclone/rclone/lib/http"
	"github.com/rclone/rclone/lib/http/auth"
	"github.com/spf13/cobra"
)

func init() {
	flagSet := commandDefinition.Flags()

	flags.StringArrayVarP(flagSet, &addresses, "rcd2-addrs", "", addresses, "list of addresses")

	libhttp.AddFlagsPrefix(flagSet, "rcd2-", &HTTPOptions)
	auth.AddFlagsPrefix(flagSet, "rcd2-", &AuthOptions)

	cmd.Root.AddCommand(commandDefinition)
}

var (
	addresses    []string
	listeners    []net.Listener
	tlsListeners []net.Listener
	AuthOptions  auth.Options
	HTTPOptions  = libhttp.DefaultOpt
)

var commandDefinition = &cobra.Command{
	Use:   "rcd2 <path to files to serve>*",
	Short: `Run rclone listening to remote control commands only.`,
	Long: `
This runs rclone so that it only listens to remote control commands.

This is useful if you are controlling rclone via the rc API.

If you pass in a path to a directory, rclone will serve that directory
for GET requests on the URL passed in.  It will also open the URL in
the browser when rclone is run.

See the [rc documentation](/rc/) for more info on the rc flags.
`,
	Run: func(command *cobra.Command, args []string) {
		// cmd.CheckArgs(0, 1, command, args)
		// if rcflags.Opt.Enabled {
		// 	log.Fatalf("Don't supply --rc flag when using rcd")
		// }

		// Start the rc
		// rcflags.Opt.Enabled = true
		// if len(args) > 0 {
		// 	rcflags.Opt.Files = args[0]
		// }

		for _, aa := range addresses {
			target := &listeners
			if strings.HasPrefix(aa, "tls://") {
				aa = strings.TrimPrefix(aa, "tls://")
				target = &tlsListeners
			}

			l, err := NewListener(aa)
			if err != nil {
				log.Fatalf("invalid address %q: %v", aa, err)
			}
			// keep this so unix sockets get cleaned up on early exit
			defer l.Close()

			*target = append(*target, l)
		}

		s, err := libhttp.NewServer(listeners, tlsListeners, HTTPOptions)
		if err != nil {
			log.Fatalf("error starting server: %v", err)
		}

		s.Mount("/", Handler())

		s.Serve()

		// Notify stopping on exit
		var finaliseOnce sync.Once
		finalise := func() {
			finaliseOnce.Do(func() {
				_ = sysdnotify.Stopping()
				err := s.Shutdown()
				if err != nil {
					log.Printf("error shutting down server: %v", err)
				}
			})
		}
		fnHandle := atexit.Register(finalise)
		defer atexit.Unregister(fnHandle)

		// Notify ready to systemd
		if err := sysdnotify.Ready(); err != nil {
			log.Fatalf("failed to notify ready to systemd: %v", err)
		}

		s.Wait()
		finalise()
	},
}

// func old(command *cobra.Command, args []string) {
// 	cmd.CheckArgs(0, 1, command, args)
// 	if rcflags.Opt.Enabled {
// 		log.Fatalf("Don't supply --rc flag when using rcd")
// 	}

// 	// Start the rc
// 	rcflags.Opt.Enabled = true
// 	if len(args) > 0 {
// 		rcflags.Opt.Files = args[0]
// 	}

// 	s, err := rcserver.Start(context.Background(), &rcflags.Opt)
// 	if err != nil {
// 		log.Fatalf("Failed to start remote control: %v", err)
// 	}
// 	if s == nil {
// 		log.Fatal("rc server not configured")
// 	}

// 	// Notify stopping on exit
// 	var finaliseOnce sync.Once
// 	finalise := func() {
// 		finaliseOnce.Do(func() {
// 			_ = sysdnotify.Stopping()
// 		})
// 	}
// 	fnHandle := atexit.Register(finalise)
// 	defer atexit.Unregister(fnHandle)

// 	// Notify ready to systemd
// 	if err := sysdnotify.Ready(); err != nil {
// 		log.Fatalf("failed to notify ready to systemd: %v", err)
// 	}

// 	s.Wait()
// 	finalise()
// }
