// Package caveman adapts the pinned Caveman proxy to the provider-neutral
// compression lifecycle. It never invokes Caveman's global installer, hooks,
// skills, memory, browse, learn or pixel features.
package caveman

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const (
	implementation     = "caveman"
	healthSchema       = "caveman.proxy.health.v1"
	versionSchema      = "caveman.proxy.run.v1"
	defaultStartupWait = 10 * time.Second
	maxInlineConfig    = 4 << 20
)

type Provider struct {
	Binary         string
	SupplyRoot     string
	Expected       supplychain.ResolvedSource
	Managed        bool
	Runner         platform.Runner
	HTTPClient     *http.Client
	StartupTimeout time.Duration
	ProxyArgs      []string
	IntegrityCheck func() error
	Port           func() (int, error)
}

func (p Provider) ID() core.ComponentID { return core.ComponentCompression }

type versionDocument struct {
	Schema       string   `json:"schema"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

func (p Provider) Probe(ctx context.Context) core.ComponentStatus {
	status := core.ComponentStatus{
		ID: core.ComponentCompression, Implementation: implementation, Active: true,
		Managed: p.Managed, Health: core.HealthUnavailable, Lifecycle: core.LifecycleStopped,
		Provenance:    core.Provenance{Source: "managed_supply_chain", Version: p.Expected.LogicalVersion, Path: p.Binary},
		Capabilities:  core.CapabilitySet{core.CapabilityCompressionWrap: core.SupportUnsupported, core.CapabilityCompressionBypass: core.SupportSupported},
		Compatibility: core.Compatibility{State: core.CompatibilityUnknown},
		Fallback:      core.Fallback{Allowed: true, Reason: "direct official executor remains available before launch"},
	}
	if err := p.validateManaged(); err != nil {
		status.Compatibility = core.Compatibility{State: core.CompatibilityIncompatible, Reason: "managed runtime integrity validation failed"}
		return status
	}
	status.Installed = true
	runner := p.Runner
	if runner == nil {
		runner = platform.ExecRunner{}
	}
	result, err := runner.Run(ctx, p.Binary, []string{"version", "--json"}, platform.RunOptions{Timeout: 10 * time.Second})
	if err != nil {
		status.Health = core.HealthDegraded
		status.Compatibility = core.Compatibility{State: core.CompatibilityUnknown, Reason: "structured version probe failed"}
		return status
	}
	var document versionDocument
	if json.Unmarshal([]byte(result.Stdout), &document) != nil || document.Schema != versionSchema || !contains(document.Capabilities, "run_state") {
		status.Health = core.HealthDegraded
		status.Compatibility = core.Compatibility{State: core.CompatibilityIncompatible, Reason: "structured version probe is incompatible"}
		return status
	}
	// The reviewed bin-v1.1.3 assets report "dev". Immutable supply-chain
	// revision and digest remain authoritative; never relabel this as a runtime-
	// verified semantic version.
	if strings.TrimSpace(document.Version) != "" && document.Version != "dev" {
		status.Provenance.Version = document.Version
	}
	status.Available, status.Health = true, core.HealthHealthy
	status.Compatibility = core.Compatibility{State: core.CompatibilityCompatible}
	status.Capabilities[core.CapabilityCompressionWrap] = core.SupportSupported
	return status
}

func (p Provider) Prepare(ctx context.Context, request core.CompressionRequest) (core.CompressionLease, error) {
	direct := core.CompressionDecision{Command: request.DirectPath, Args: append([]string(nil), request.Args...), Environment: cloneEnvironment(request.Environment)}
	if request.Fidelity == core.CompressionExactRequired || request.Fidelity == core.CompressionBypass || request.Fidelity == core.CompressionUnsupported {
		return fixedLease{decision: direct}, nil
	}
	if request.Fidelity != "" && request.Fidelity != core.CompressionCompressible {
		return nil, fmt.Errorf("unsupported compression fidelity %q", request.Fidelity)
	}
	if request.Executor != core.ComponentCodex && request.Executor != core.ComponentClaude && request.Executor != core.ComponentOpenCode {
		return nil, fmt.Errorf("Caveman does not support executor %q", request.Executor)
	}
	if request.RuntimeDir == "" || !filepath.IsAbs(request.RuntimeDir) {
		return nil, errors.New("Caveman requires an absolute session runtime directory")
	}
	if err := p.validateManaged(); err != nil {
		return nil, err
	}
	if status := p.Probe(ctx); !status.Available || status.Health != core.HealthHealthy {
		return nil, errors.New("Caveman structured preflight is unavailable")
	}
	lease, endpoint, err := p.start(ctx, request.RuntimeDir)
	if err != nil {
		return nil, err
	}
	decision, err := prepareExecutor(request, endpoint)
	if err != nil {
		_ = lease.Close(context.Background())
		return nil, err
	}
	decision.Used, decision.Provider = true, implementation
	lease.decision = decision
	return lease, nil
}

func (p Provider) validateManaged() error {
	info, err := os.Lstat(p.Binary)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o700 {
		return errors.New("Caveman binary is missing or unsafe")
	}
	if !p.Managed {
		return errors.New("external Caveman installations are not activated automatically")
	}
	if p.IntegrityCheck != nil {
		return p.IntegrityCheck()
	}
	if p.SupplyRoot == "" || p.Expected.ID != "caveman" {
		return errors.New("Caveman managed provenance is unavailable")
	}
	active, root, err := (supplychain.Manager{Root: p.SupplyRoot}).Active("caveman")
	if err != nil {
		return fmt.Errorf("validate Caveman active object: %w", err)
	}
	if !reflect.DeepEqual(active, p.Expected) {
		return errors.New("Caveman active provenance does not match the pinned source")
	}
	want := filepath.Join(root, filepath.FromSlash(active.PayloadPath))
	if filepath.Clean(want) != filepath.Clean(p.Binary) {
		return errors.New("Caveman state path diverges from the active immutable object")
	}
	return nil
}

func (p Provider) start(ctx context.Context, sessionRoot string) (*processLease, string, error) {
	root := filepath.Join(sessionRoot, "caveman")
	if err := platform.EnsurePrivateDir(root); err != nil {
		return nil, "", err
	}
	directory, err := os.MkdirTemp(root, "proxy-")
	if err != nil {
		return nil, "", err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		_ = os.RemoveAll(directory)
		return nil, "", err
	}
	timeout := p.StartupTimeout
	if timeout <= 0 {
		timeout = defaultStartupWait
	}
	args := append([]string(nil), p.ProxyArgs...)
	if len(args) == 0 {
		args = []string{"serve"}
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		allocatePort := p.Port
		if allocatePort == nil {
			allocatePort = availablePort
		}
		port, portErr := allocatePort()
		if portErr != nil {
			lastErr = portErr
			continue
		}
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		configPath := filepath.Join(directory, "caveman.yaml")
		configBody := []byte("mode: compress\nlisten: " + address + "\nsubscription_compress: live_zone\ntoolschema_strip: off\nbreakpoint_plan: off\n")
		if err := platform.AtomicWritePrivate(configBody, configPath); err != nil {
			lastErr = err
			continue
		}
		command := exec.Command(p.Binary, args...)
		command.Stdout, command.Stderr = io.Discard, io.Discard
		command.Env = []string{
			"HOME=" + directory,
			"CAVEMAN_HOME=" + directory,
			"CAVEMAN_CONFIG=" + configPath,
			"CAVEMAN_PROXY_OWNER=wrap",
		}
		if err := command.Start(); err != nil {
			lastErr = err
			continue
		}
		lease := newProcessLease(command, directory)
		endpoint := "http://" + address
		if err := p.waitReady(ctx, lease, endpoint, timeout); err != nil {
			lastErr = err
			_ = lease.Close(context.Background())
			continue
		}
		return lease, endpoint, nil
	}
	_ = os.RemoveAll(directory)
	return nil, "", fmt.Errorf("Caveman proxy did not become ready: %w", lastErr)
}

func (p Provider) waitReady(ctx context.Context, lease *processLease, endpoint string, timeout time.Duration) error {
	client := p.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 500 * time.Millisecond}
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-lease.Done():
			if err == nil {
				err = errors.New("proxy exited")
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("readiness timeout")
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/health/ready", nil)
			response, err := client.Do(req)
			if err != nil {
				continue
			}
			var body struct {
				OK      bool   `json:"ok"`
				Service string `json:"service"`
				Schema  string `json:"schema"`
			}
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4097)).Decode(&body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && body.OK && body.Service == "caveman-proxy" && body.Schema == healthSchema {
				return nil
			}
		}
	}
}

type fixedLease struct{ decision core.CompressionDecision }

func (l fixedLease) Decision() core.CompressionDecision { return l.decision }
func (fixedLease) Done() <-chan error                   { return nil }
func (fixedLease) Close(context.Context) error          { return nil }

type processLease struct {
	decision  core.CompressionDecision
	command   *exec.Cmd
	directory string
	done      chan error
	wait      chan error
	mu        sync.Mutex
	closed    bool
}

func newProcessLease(command *exec.Cmd, directory string) *processLease {
	lease := &processLease{command: command, directory: directory, done: make(chan error, 1), wait: make(chan error, 1)}
	go func() {
		err := command.Wait()
		lease.wait <- err
		lease.done <- err
		close(lease.wait)
		close(lease.done)
	}()
	return lease
}

func (l *processLease) Decision() core.CompressionDecision { return l.decision }
func (l *processLease) Done() <-chan error                 { return l.done }

func (l *processLease) Close(ctx context.Context) error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()
	if l.command.Process != nil {
		_ = l.command.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-l.wait:
	case <-ctx.Done():
		_ = l.command.Process.Kill()
		<-l.wait
	case <-time.After(2 * time.Second):
		_ = l.command.Process.Kill()
		<-l.wait
	}
	info, err := os.Lstat(l.directory)
	if err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		_ = os.RemoveAll(l.directory)
	}
	return nil
}

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, err
	}
	return port, nil
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
