package codexresolver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/platform"
)

type client struct {
	version                               string
	broken, unauthenticated, noCapability bool
}
type runner struct {
	direct  string
	clients map[string]client
	calls   []string
}

func (r *runner) LookPath(string) (string, error) { return r.direct, nil }
func (r *runner) Run(_ context.Context, path string, args []string, _ platform.RunOptions) (platform.Result, error) {
	r.calls = append(r.calls, path+":"+strings.Join(args, " "))
	c, ok := r.clients[path]
	if !ok || c.broken {
		return platform.Result{}, errors.New("broken")
	}
	switch strings.Join(args, " ") {
	case "--version":
		return platform.Result{Stdout: "codex-cli " + c.version}, nil
	case "login status":
		if c.unauthenticated {
			return platform.Result{Stdout: "Not logged in"}, nil
		}
		return platform.Result{Stderr: "Logged in using ChatGPT"}, nil
	default:
		if c.noCapability {
			return platform.Result{}, errors.New("unsupported")
		}
		return platform.Result{Stdout: "Usage: codex"}, nil
	}
}

func binary(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fixture executable"), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolutionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, user, managed, want                           string
		userBad, managedBad, userAuthBad, userCapabilityBad bool
	}{
		{name: "user newer", user: "0.153.4", managed: "0.148.0", want: "user/system"},
		{name: "managed newer", user: "0.148.0", managed: "0.153.4", want: "managed"},
		{name: "same version prefers user", user: "0.153.4", managed: "0.153.4", want: "user/system"},
		{name: "incompatible user", user: "0.147.0", managed: "0.148.0", want: "managed"},
		{name: "broken user", user: "0.153.4", managed: "0.148.0", userBad: true, want: "managed"},
		{name: "broken managed", user: "0.153.4", managed: "0.148.0", managedBad: true, want: "user/system"},
		{name: "no user", managed: "0.148.0", want: "managed"},
		{name: "no managed", user: "0.153.4", want: "user/system"},
		{name: "neither"},
		{name: "prerelease excluded", user: "0.154.0-alpha.1", managed: "0.148.0", want: "managed"},
		{name: "parse failure", user: "nonsense", managed: "0.148.0", want: "managed"},
		{name: "user unauthenticated", user: "0.153.4", managed: "0.148.0", userAuthBad: true, want: "managed"},
		{name: "user capability missing", user: "0.153.4", managed: "0.148.0", userCapabilityBad: true, want: "managed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			run := &runner{clients: map[string]client{}}
			resolver := Resolver{Runner: run}
			if tc.user != "" {
				run.direct = binary(t, root, "user/codex")
				run.clients[run.direct] = client{version: tc.user, broken: tc.userBad, unauthenticated: tc.userAuthBad, noCapability: tc.userCapabilityBad}
			}
			if tc.managed != "" {
				path := binary(t, root, "managed/codex")
				resolver.Managed = config.ComponentState{Path: path, Managed: true, Installed: true, Version: tc.managed}
				resolver.Host = config.ComponentState{Path: binary(t, root, "managed/codex-code-mode-host"), Managed: true, Installed: true, Version: tc.managed}
				run.clients[path] = client{version: tc.managed, broken: tc.managedBad}
			}
			got, err := resolver.Resolve(context.Background())
			if tc.want == "" {
				if err == nil {
					t.Fatal("accepted missing client")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Effective.Ownership != tc.want {
				t.Fatalf("got %+v", got)
			}
			if got.LatestStable != "NOT_CHECKED" {
				t.Fatal("launch queried upstream")
			}
			if tc.user != "" && got.Direct.Version != "" && got.Direct.Version != got.Effective.Version && !got.VersionDrift {
				t.Fatal("silent drift")
			}
		})
	}
}

func TestMultiplePATHAndSymlinkFreeze(t *testing.T) {
	root := t.TempDir()
	old := binary(t, root, "old/codex")
	newer := binary(t, root, "version1/codex")
	link := filepath.Join(root, "codex")
	if err := os.Symlink(newer, link); err != nil {
		t.Fatal(err)
	}
	run := &runner{direct: old, clients: map[string]client{old: {version: "0.148.0"}, newer: {version: "0.153.4"}}}
	resolver := Resolver{Runner: run, PATH: filepath.Dir(old) + ":" + root + ":" + root + ":.:"}
	got, err := resolver.Resolve(context.Background())
	if err != nil || got.Effective.RealPath != newer || len(got.Candidates) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	// An upgrade or rollback of the user's symlink does not mutate an active
	// session. A subsequent launch gets the new target.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(old, link); err != nil {
		t.Fatal(err)
	}
	next, err := resolver.Resolve(context.Background())
	if err != nil || next.Effective.Version != "0.148.0" || got.Effective.Version != "0.153.4" || got.Effective.RealPath != newer {
		t.Fatalf("session was not frozen: %+v %+v %v", got, next, err)
	}
}

func TestApplyPreservesManagedRollbackState(t *testing.T) {
	state := config.State{Components: map[string]config.ComponentState{"codex": {Path: "/managed/codex", Version: "0.148.0", Managed: true}}}
	r := Resolution{Effective: Candidate{Path: "/user/codex", RealPath: "/versions/new/codex", Version: "0.153.4", Ownership: "user/system"}}
	effective := r.Apply(state)
	if effective.Components["codex"].Path != "/versions/new/codex" || state.Components["codex"].Path != "/managed/codex" || !state.Components["codex"].Managed {
		t.Fatal("selection mutated persistent installation/rollback state")
	}
}

type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func TestUpstreamOfflineUnavailableAndStaleObservation(t *testing.T) {
	for _, body := range []string{"offline", "503", `{"tag_name":"rust-v0.154.0-alpha.1","prerelease":true}`, `{broken`, `{"tag_name":"rust-v0.153.4","prerelease":false}`} {
		t.Run(body, func(t *testing.T) {
			r := Resolution{LatestStable: "0.999.0", Effective: Candidate{Version: "0.153.4", Path: "/validated/codex"}}
			r.CheckLatest(context.Background(), &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				if body == "offline" {
					return nil, errors.New("offline")
				}
				status := 200
				if body == "503" {
					status = 503
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			if r.Effective.Path != "/validated/codex" {
				t.Fatal("network changed selected executable")
			}
			want := "UNKNOWN_OFFLINE"
			if strings.Contains(body, `"prerelease":false`) {
				want = "0.153.4"
			}
			if r.LatestStable != want {
				t.Fatalf("stale upstream observation used: %+v", r)
			}
		})
	}
}

func TestVersionRejectsAmbiguousAndPreReleaseValues(t *testing.T) {
	for _, v := range []string{"0.148.0-alpha", "0.148.0+build", "0.148", "01.148.0", "codex 0.148.0", "999999999999999999999.0.0", "0.148.0 extra"} {
		if _, ok := Version(v); ok {
			t.Fatalf("accepted %q", v)
		}
	}
}

func TestManagedCompanionRequired(t *testing.T) {
	root := t.TempDir()
	path := binary(t, root, "codex")
	run := &runner{clients: map[string]client{path: {version: "0.153.4"}}}
	r := Resolver{Runner: run, Managed: config.ComponentState{Path: path, Managed: true, Installed: true}}
	got, err := r.Resolve(context.Background())
	if err == nil || got.Managed.Reason != "managed_companion_incompatible" {
		t.Fatalf("%+v %v", got, err)
	}
}
