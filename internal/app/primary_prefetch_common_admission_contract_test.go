package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestPrimaryPrefetchCommonAdmissionClosedFailures(t *testing.T) {
	for _, mode := range []string{"policy", "missing_transport", "pin", "auth", "requirements", "route", "rpc", "cleanup", "nonzero", "not_reaped", "scope", "inventory_error", "inventory_partial", "inventory_duplicate", "inventory_mutation", "inventory_argv_mutation", "inventory_dir_mutation", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			original := subscriptionFixturePlan(t)
			// Constructor accepts only the base shared profile, never existing overrides.
			base := original.scope
			base.globals = append([]string{}, base.globals[:len(base.globals)-6]...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			policy := prefetchSubscriptionHostPolicy{PermitFilteredEnvironment: mode != "policy"}
			probes, inventories := 0, 0
			probe := prefetchCommonProbe(func(ctx context.Context, p prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error) {
				probes++
				s := &subscriptionFixtureSession{values: subscriptionFixtureValues()}
				switch mode {
				case "auth":
					s.values[4]["account"] = nil
				case "requirements":
					s.values[3]["requirements"] = map[string]any{"unknown": "secret-canary"}
				case "route":
					s.values[4]["workspaceRouting"].(map[string]any)["backendOrigin"] = "https://synthetic.invalid"
				case "rpc":
					s.callErr = errors.New("secret-canary")
				case "cleanup":
					s.closeErr = errors.New("secret-canary")
				}
				a, e := prefetchSubscriptionProbe(ctx, p, func(context.Context, *exec.Cmd) (prefetchSubscriptionSession, error) { return s, nil })
				life := prefetchCommonLifecycle{reaped: mode != "not_reaped", nativeWaitZero: mode != "nonzero"}
				if mode == "scope" && a != nil {
					a.plan.scope.environment = append(a.plan.scope.environment, "HOME=/forged")
				}
				return a, life, e
			})
			if mode == "missing_transport" {
				probe = nil
			}
			run := func(_ context.Context, cmd *exec.Cmd) error {
				inventories++
				if mode == "inventory_error" {
					return errors.New("secret-canary")
				}
				body := `[{"name":"synthetic_user"},{"name":"synthetic_system"}]`
				if mode == "inventory_partial" {
					body = `[{"name":`
				}
				if mode == "inventory_duplicate" {
					body = `[{"name":"synthetic_user","name":"synthetic_other"}]`
				}
				if mode == "inventory_mutation" {
					cmd.Env = append(cmd.Env, "HOME=/forged")
				}
				if mode == "inventory_argv_mutation" {
					cmd.Args[1] = "--forged"
				}
				if mode == "inventory_dir_mutation" {
					cmd.Dir = "/forged"
				}
				_, e := io.WriteString(cmd.Stdout, body)
				return e
			}
			if mode == "cancel" {
				cancel()
			}
			permit, observed := prefetchCommonPrepare(ctx, base, policy, probe, run, func(prefetchDirectScope) bool { return mode != "pin" })
			if permit != nil || observed.State != "NOT_RUN" || observed.ModelAttempts != 0 || observed.AutomaticFallbacks != 0 {
				t.Fatal("refusal admitted a model or retained replay eligibility")
			}
			inventoryCase := strings.HasPrefix(mode, "inventory_")
			if inventoryCase && inventories != 1 || !inventoryCase && inventories != 0 || probes > 1 {
				t.Fatal("failed precondition advanced to a later execution stage")
			}
			body, _ := json.Marshal(observed)
			if strings.Contains(string(body), "secret-canary") || strings.Contains(string(body), "/forged") {
				t.Fatal("closed observation retained raw data")
			}
		})
	}
}

func TestPrimaryPrefetchCommonAdmissionScopeAndNoReplay(t *testing.T) {
	p := subscriptionFixturePlan(t)
	base := p.scope
	base.globals = append([]string{}, base.globals[:len(base.globals)-6]...)
	for _, arm := range []string{"A", "B"} {
		t.Run(arm, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := []string{}
			var probeCmd *exec.Cmd
			probe := func(ctx context.Context, p prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error) {
				calls = append(calls, "probe")
				probeCmd = p.probeCommand(ctx)
				s := &subscriptionFixtureSession{values: subscriptionFixtureValues()}
				a, e := prefetchSubscriptionProbe(ctx, p, func(context.Context, *exec.Cmd) (prefetchSubscriptionSession, error) { return s, nil })
				return a, prefetchCommonLifecycle{true, true}, e
			}
			permit, observed := prefetchCommonPrepare(ctx, base, prefetchSubscriptionHostPolicy{true}, probe, func(_ context.Context, cmd *exec.Cmd) error {
				calls = append(calls, "inventory")
				if cmd.Path != probeCmd.Path || cmd.Dir != probeCmd.Dir || !reflect.DeepEqual(cmd.Env, probeCmd.Env) || !reflect.DeepEqual(cmd.Args[:len(cmd.Args)-3], probeCmd.Args[:len(probeCmd.Args)-3]) {
					t.Fatal("three-pass scope diverged")
				}
				_, e := io.WriteString(cmd.Stdout, `[{"name":"synthetic_user"},{"name":"synthetic_system"}]`)
				return e
			}, func(prefetchDirectScope) bool { return true })
			if permit == nil || observed.State != "ADMITTED" || observed.ModelAttempts != 0 || !reflect.DeepEqual(calls, []string{"probe", "inventory"}) {
				t.Fatal("fresh composed checks did not run before command construction")
			}
			if strings.Contains(fmt.Sprintf("%+v %#v", permit, permit), "synthetic/private-home") {
				t.Fatal("scope leaked through formatting")
			}
			prompt := "--profile hostile -c model_provider=custom\nsynthetic-question"
			command, e := permit.takeCommand(ctx, "/synthetic/schema", prompt)
			if e != nil || command.Dir != probeCmd.Dir || !reflect.DeepEqual(command.Env, probeCmd.Env) || !reflect.DeepEqual(command.Args[len(command.Args)-2:], []string{"--", prompt}) {
				t.Fatal("prompt escaped data boundary or scope changed")
			}
			for _, name := range []string{"synthetic_user", "synthetic_system"} {
				n := 0
				for _, arg := range command.Args {
					if arg == "mcp_servers."+name+".enabled=false" {
						n++
					}
				}
				if n != 1 {
					t.Fatal("deny-only inventory entry missing")
				}
			}
			if _, e = permit.takeCommand(ctx, "/synthetic/schema", prompt); e == nil {
				t.Fatal("eligibility replayed")
			}
			if _, e = (*prefetchCommonAdmission)(nil).takeCommand(ctx, "x", "x"); e == nil {
				t.Fatal("nil eligibility admitted command")
			}
		})
	}
}

func TestPrimaryPrefetchCommonAdmissionPostProbeAndCommandChanges(t *testing.T) {
	for _, mode := range []string{"pin_changed", "scope_changed", "cancelled", "wrong_context"} {
		t.Run(mode, func(t *testing.T) {
			p := subscriptionFixturePlan(t)
			base := p.scope
			base.globals = append([]string{}, base.globals[:len(base.globals)-6]...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			verifyCalls := 0
			permit, obs := prefetchCommonPrepare(ctx, base, prefetchSubscriptionHostPolicy{true}, func(ctx context.Context, p prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error) {
				a, e := prefetchSubscriptionProbe(ctx, p, func(context.Context, *exec.Cmd) (prefetchSubscriptionSession, error) {
					return &subscriptionFixtureSession{values: subscriptionFixtureValues()}, nil
				})
				return a, prefetchCommonLifecycle{true, true}, e
			}, func(_ context.Context, c *exec.Cmd) error { _, e := io.WriteString(c.Stdout, `[]`); return e }, func(prefetchDirectScope) bool { verifyCalls++; return mode != "pin_changed" || verifyCalls < 3 })
			if permit == nil || obs.State != "ADMITTED" {
				t.Fatal("controlled precondition missing")
			}
			if mode == "scope_changed" {
				permit.inventory.scope.directory = "/forged"
			}
			if mode == "cancelled" {
				cancel()
			}
			commandCtx := ctx
			if mode == "wrong_context" {
				commandCtx = context.WithValue(ctx, struct{}{}, true)
			}
			if cmd, e := permit.takeCommand(commandCtx, "/schema", "question"); e == nil || cmd != nil {
				t.Fatal("changed admission started a model")
			}
			if cmd, e := permit.takeCommand(ctx, "/schema", "question"); e == nil || cmd != nil {
				t.Fatal("failed eligibility attempt could be replayed")
			}
		})
	}
}
