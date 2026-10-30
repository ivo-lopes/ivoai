package app

// Private benchmark launch eligibility only. This adds no production authority,
// account store, mutable replay cache or automatic provider failover.
import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"reflect"
	"time"
)

//go:embed primary_prefetch_common_admission_linux_test.go primary_prefetch_common_admission_other_test.go primary_prefetch_common_admission_test.go primary_prefetch_live_test.go primary_prefetch_subscription_guard_test.go primary_prefetch_subscription_stdio_linux_test.go testdata/prefetch-admission-contract.json
var prefetchCommonSources embed.FS

type prefetchCommonCapabilities struct {
	Version              int               `json:"version"`
	Mode                 string            `json:"mode"`
	SourceWired          bool              `json:"source_wired"`
	Files                map[string]string `json:"files"`
	SourceSHA256         string            `json:"source_sha256"`
	DriverContractSHA256 string            `json:"driver_contract_sha256"`
}

// Entirely embedded source/contract data; no HOME, auth, profile or client read.
func prefetchCommonContract() prefetchCommonCapabilities {
	body, _ := prefetchCommonSources.ReadFile("testdata/prefetch-admission-contract.json")
	var spec struct {
		Version     int      `json:"version"`
		Mode        string   `json:"mode"`
		SourceWired bool     `json:"source_wired"`
		SourceFiles []string `json:"source_files"`
	}
	_ = json.Unmarshal(body, &spec)
	result := prefetchCommonCapabilities{Version: spec.Version, Mode: spec.Mode, SourceWired: spec.SourceWired, Files: map[string]string{}}
	contract := sha256.Sum256(body)
	result.DriverContractSHA256 = hex.EncodeToString(contract[:])
	combined := sha256.New()
	for _, name := range spec.SourceFiles {
		data, _ := prefetchCommonSources.ReadFile(name)
		digest := sha256.Sum256(data)
		result.Files[name] = hex.EncodeToString(digest[:])
		combined.Write([]byte(name + "\x00"))
		combined.Write(data)
		combined.Write([]byte{0})
	}
	result.SourceSHA256 = hex.EncodeToString(combined.Sum(nil))
	return result
}

type prefetchCommonObservation struct {
	Version            int    `json:"version"`
	SourceWired        bool   `json:"source_wired"`
	State              string `json:"state"`
	Reason             string `json:"reason"`
	ProbeAttempts      int    `json:"probe_attempts"`
	InventoryAttempts  int    `json:"inventory_attempts"`
	ModelAttempts      int    `json:"model_attempts"`
	AutomaticFallbacks int    `json:"automatic_fallbacks"`
	OwnedReaped        bool   `json:"owned_reaped"`
	NativeWaitZero     bool   `json:"native_wait_zero"`
	ScopeMatched       bool   `json:"scope_matched"`
	ProbeMS            int64  `json:"probe_ms"`
	InventoryMS        int64  `json:"inventory_ms"`
}

func prefetchCommonNotRun() prefetchCommonObservation {
	return prefetchCommonObservation{Version: 1, SourceWired: true, State: "NOT_RUN", Reason: "NOT_ATTEMPTED"}
}

type prefetchCommonLifecycle struct{ reaped, nativeWaitZero bool }
type prefetchCommonProbe func(context.Context, prefetchSubscriptionPlan) (*prefetchSubscriptionAdmission, prefetchCommonLifecycle, error)
type prefetchCommonVerifier func(prefetchDirectScope) bool
type prefetchCommonInventoryRunner func(context.Context, *exec.Cmd) error

// Scope is private RAM only: String/GoString/JSON never disclose environment,
// server names, argv, paths or config/account data.
type prefetchCommonAdmission struct {
	ctx       context.Context
	plan      prefetchSubscriptionPlan
	inventory *prefetchDirectInventoryResult
	verify    prefetchCommonVerifier
	used      bool
}

func (*prefetchCommonAdmission) String() string   { return "private-benchmark-launch-eligibility" }
func (*prefetchCommonAdmission) GoString() string { return "private-benchmark-launch-eligibility" }
func (*prefetchCommonAdmission) MarshalJSON() ([]byte, error) {
	return []byte(`{"source_wired":true}`), nil
}

// Injected callbacks are for controlled tests only. The live entrypoint below
// supplies the literal pinned official constructor, never a config/model seam.
func prefetchCommonPrepare(ctx context.Context, scope prefetchDirectScope, policy prefetchSubscriptionHostPolicy, probe prefetchCommonProbe, run prefetchCommonInventoryRunner, verify prefetchCommonVerifier) (*prefetchCommonAdmission, prefetchCommonObservation) {
	observation := prefetchCommonNotRun()
	deny := func(reason string) (*prefetchCommonAdmission, prefetchCommonObservation) {
		observation.Reason = reason
		return nil, observation
	}
	if ctx == nil || ctx.Err() != nil {
		return deny("CANCELED")
	}
	plan, err := prefetchPrepareSubscription(scope, policy)
	if err != nil {
		return deny(err.Error())
	}
	if probe == nil || run == nil || verify == nil {
		return deny("TRANSPORT_UNAVAILABLE")
	}
	if !verify(plan.inventoryScope()) {
		return deny("PIN_DENIED")
	}
	observation.ProbeAttempts = 1
	start := time.Now()
	route, life, err := probe(ctx, prefetchSubscriptionPlan{scope: plan.inventoryScope()})
	observation.ProbeMS = time.Since(start).Milliseconds()
	observation.OwnedReaped, observation.NativeWaitZero = life.reaped, life.nativeWaitZero
	if err != nil {
		reason := "RPC_ERROR"
		if closed, ok := err.(prefetchSubscriptionReason); ok {
			reason = closed.Error()
		}
		return deny(reason)
	}
	if ctx.Err() != nil {
		return deny("CANCELED")
	}
	if !life.reaped || !life.nativeWaitZero {
		return deny("LIFECYCLE_DENIED")
	}
	if route == nil || !route.summary.Accepted || route.summary.Connected || route.summary.Provider != "OPENAI" || route.summary.Auth != "CHATGPT_COMPATIBLE" || route.summary.Route != "CHATGPT_CANONICAL" || route.summary.Reason != "ACCEPTED_UNQUALIFIED" || !reflect.DeepEqual(route.plan.scope, plan.scope) {
		return deny("SCOPE_DENIED")
	}
	if !verify(plan.inventoryScope()) {
		return deny("PIN_DENIED")
	}
	observation.InventoryAttempts = 1
	start = time.Now()
	inventoryCtx, cancelInventory := context.WithTimeout(ctx, 15*time.Second)
	inventory, err := prefetchDirectInventory(inventoryCtx, plan.inventoryScope(), func(cmd *exec.Cmd) error { return run(inventoryCtx, cmd) })
	cancelInventory()
	observation.InventoryMS = time.Since(start).Milliseconds()
	if err != nil {
		return deny("INVENTORY_FAILED")
	}
	if ctx.Err() != nil {
		return deny("CANCELED")
	}
	if inventory == nil || !reflect.DeepEqual(inventory.scope, plan.scope) {
		return deny("SCOPE_DENIED")
	}
	observation.State, observation.Reason, observation.ScopeMatched = "ADMITTED", "ACCEPTED_UNQUALIFIED", true
	return &prefetchCommonAdmission{ctx: ctx, plan: plan, inventory: inventory, verify: verify}, observation
}

func (p *prefetchCommonAdmission) takeCommand(ctx context.Context, schema, prompt string) (*exec.Cmd, error) {
	if p == nil || p.used {
		return nil, prefetchSubscriptionScopeDenied
	}
	p.used = true // Failure consumes this eligibility too; no replay/retry.
	if p.ctx == nil || ctx != p.ctx || ctx.Err() != nil || p.inventory == nil || p.verify == nil {
		return nil, prefetchSubscriptionScopeDenied
	}
	if !reflect.DeepEqual(p.inventory.scope, p.plan.scope) {
		return nil, prefetchSubscriptionScopeDenied
	}
	if !p.verify(p.plan.inventoryScope()) {
		return nil, prefetchSubscriptionScopeDenied
	}
	return prefetchDirectExec(ctx, p.inventory, schema, prompt)
}
