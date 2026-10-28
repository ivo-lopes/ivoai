package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/admin"
	"github.com/ivo-lopes/ivoai/internal/app"
)

func runAdmin(ctx context.Context, a *app.App, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ivoai admin serve [--listen 127.0.0.1:7781] | access <create|list|revoke>")
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("admin serve", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		address := fs.String("listen", "127.0.0.1:7781", "numeric loopback IP:port; no remote access")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errors.New("invalid admin listener options")
		}
		host, _, err := net.SplitHostPort(*address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("admin listener requires a numeric loopback IP")
		}
		listener, err := net.Listen("tcp", *address)
		if err != nil {
			return errors.New("admin listener unavailable")
		}
		defer listener.Close()
		// Serialize metadata logging independently of App.Out/Err and never attach
		// request URLs, query parameters, cookies, body, codes or credentials.
		audit := newAdminAudit(a)
		server, err := admin.New(admin.Options{App: a, Origin: "http://" + listener.Addr().String(), Audit: audit})
		if err != nil {
			return err
		}
		fmt.Fprintf(a.Out, "Local read-only admin: http://%s/\nUse ivoai admin access create in another terminal.\n", listener.Addr())
		ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		return server.ServeListener(ctx, listener)
	case "access":
		if len(args) < 2 {
			return errors.New("usage: ivoai admin access <create|list|revoke>")
		}
		store, err := admin.AccessStore(a.Store.Paths)
		if err != nil {
			return err
		}
		switch args[1] {
		case "create":
			fs := flag.NewFlagSet("admin access create", flag.ContinueOnError)
			fs.SetOutput(a.Err)
			ttl := fs.Duration("ttl", 10*time.Minute, "one-time activation lifetime, maximum24h")
			if err := fs.Parse(args[2:]); err != nil || fs.NArg() != 0 {
				return errors.New("invalid admin activation options")
			}
			created, err := store.CreateActivation(*ttl, []string{admin.ScopeRead})
			if err != nil {
				return errors.New("unable to issue admin activation")
			}
			fmt.Fprintf(a.Out, "Local owner read-only access ID: %s\nExpires: %s\nActivation code (shown once): %s\n", created.ID, created.ExpiresAt.Format(time.RFC3339), created.Code)
			return nil
		case "list":
			if len(args) != 2 {
				return errors.New("usage: ivoai admin access list")
			}
			grants, err := store.ListGrants()
			if err != nil {
				return errors.New("admin grants unavailable")
			}
			for _, grant := range grants {
				fmt.Fprintf(a.Out, "%s\t%s\t%s\n", grant.ID, grant.Status, grant.ExpiresAt.Format(time.RFC3339))
			}
			return nil
		case "revoke":
			if len(args) != 3 {
				return errors.New("usage: ivoai admin access revoke <id>")
			}
			if err := store.RevokeActivation(args[2]); err != nil {
				return errors.New("admin grant revocation failed")
			}
			fmt.Fprintln(a.Out, "Local admin grant revoked.")
			return nil
		default:
			return errors.New("unknown admin access action")
		}
	default:
		return errors.New("unknown admin action")
	}
}
