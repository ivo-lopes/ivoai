package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
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
	case "init":
		if len(args) != 1 {
			return errors.New("usage: ivoai admin init")
		}
		if err := admin.Initialize(a.Store.Paths); err != nil {
			return err
		}
		fmt.Fprintln(a.Out, "Private administrative state initialized; no client, credential or service was installed.")
		return nil
	case "serve":
		fs := flag.NewFlagSet("admin serve", flag.ContinueOnError)
		fs.SetOutput(a.Err)
		address := fs.String("listen", "127.0.0.1:7781", "numeric loopback upstream IP:port")
		publicURL := fs.String("public-url", "", "explicit public HTTPS URL including optional path prefix")
		trusted := fs.String("trusted-proxies", "", "comma-separated upstream proxy peer CIDRs; required with public-url")
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
		proxies := []string{}
		if *trusted != "" {
			proxies = strings.Split(*trusted, ",")
		}

		server, err := admin.New(admin.Options{App: a, Origin: "http://" + listener.Addr().String(), PublicURL: *publicURL, TrustedProxies: proxies, Dispatch: administrativeDispatch(a), Audit: audit})
		if err != nil {
			return err
		}
		public := "http://" + listener.Addr().String() + "/"
		if *publicURL != "" {
			public = strings.TrimRight(*publicURL, "/") + "/"
		}
		fmt.Fprintf(a.Out, "Administrative console: %s\nUpstream listener: %s\nUse ivoai admin access create (read-only) or add --write for explicit management authority.\n", public, listener.Addr())
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
			manage := fs.Bool("write", false, "explicitly grant administrative changes with per-action confirmation")
			if err := fs.Parse(args[2:]); err != nil || fs.NArg() != 0 {
				return errors.New("invalid admin activation options")
			}
			scopes := []string{admin.ScopeRead}
			if *manage {
				scopes = append(scopes, admin.ScopeManage)
			}
			created, err := store.CreateActivation(*ttl, scopes)
			if err != nil {
				return errors.New("unable to issue admin activation")
			}
			fmt.Fprintf(a.Out, "Owner access ID: %s\nScopes: %s\nExpires: %s\nActivation code (shown once): %s\n", created.ID, strings.Join(scopes, " "), created.ExpiresAt.Format(time.RFC3339), created.Code)
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

// Each operation owns its IO while sharing the existing App services/store.
// The admin catalog is the only caller that constructs these arguments.
func administrativeDispatch(a *app.App) admin.Dispatch {
	return func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		operation := *a
		operation.In, operation.Out, operation.Err = in, out, errOut
		return runCommand(ctx, &operation, args)
	}
}
