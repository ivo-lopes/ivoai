package admin

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/app"
	"github.com/ivo-lopes/ivoai/internal/config"
)

// Browser fixture uses a real isolated App/Store and OAuth flow, not provider
// inference or privileged service operations. Config actions exercise the real
// App.ConfigSet. Production's complete CLI adapter is verified separately.
func liveAdministrativeFixture(t *testing.T, a *app.App) (*Server, string) {
	t.Helper()
	dispatch := Dispatch(nil)
	if os.Getenv("IVOAI_ADMIN_FIXTURE_WRITE") == "1" {
		if err := a.Store.Save(config.Default()); err != nil {
			t.Fatal(err)
		}
		dispatch = func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if len(args) == 4 && args[0] == "config" && args[1] == "set" {
				operation := *a
				operation.In, operation.Out, operation.Err = in, out, errOut
				return operation.ConfigSet(args[2], args[3])
			}
			return errors.New("operation outside isolated live fixture")
		}
	}
	upstream := httptest.NewUnstartedServer(nil)
	target, _ := url.Parse("http://" + upstream.Listener.Addr().String())
	options := Options{App: a, Origin: target.String(), Dispatch: dispatch}
	var front *httptest.Server
	if os.Getenv("IVOAI_ADMIN_FIXTURE_PROXY") == "1" {
		front = httptest.NewUnstartedServer(httputil.NewSingleHostReverseProxy(target))
		options.PublicURL = "https://" + front.Listener.Addr().String() + "/ivoai/admin"
		options.TrustedProxies = []string{"127.0.0.1/32", "::1/128"}
	}
	s, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	upstream.Config.Handler = s.Handler()
	upstream.Start()
	t.Cleanup(upstream.Close)
	if front != nil {
		front.StartTLS()
		t.Cleanup(front.Close)
		return s, options.PublicURL + "/"
	}
	return s, target.String()
}
