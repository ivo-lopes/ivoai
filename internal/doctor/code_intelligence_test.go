package doctor

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/components"
	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func TestCodeIntelligenceDoctorMissingMetadataDoesNotClaimHealth(t *testing.T) {
	d := Doctor{Store: config.NewStore(config.Paths{DataDir: t.TempDir()})}
	graft, code := d.codeIntelligence(context.Background(), config.State{}, nil, sandboxPrerequisites{LandlockABI: 6, Seccomp: true})
	if graft.Healthy || code.Component.Health != core.HealthUnavailable || code.Component.Available || code.Integrity != "metadata_unavailable" || code.Coverage != "unknown" {
		t.Fatalf("unsupported healthy claim: %+v %+v", graft, code)
	}
	matrix := componentMatrix(config.Default(), config.State{}, Report{CodeIntelligence: code})
	if len(matrix.Entries(core.ComponentID("code_intelligence"))) != 1 {
		t.Fatal("native capability absent from doctor matrix")
	}
	body, _ := json.Marshal(code)
	if strings.Contains(string(body), "enable") {
		t.Fatal("native capability exposed as operator toggle")
	}
}

func TestCodeIntelligenceDoctorDoesNotTrustUnmanagedOrFixtureState(t *testing.T) {
	d := Doctor{Store: config.NewStore(config.Paths{DataDir: t.TempDir()})}
	spec := components.Spec{Name: "graft"}
	for _, state := range []config.ComponentState{{Installed: true, Version: "fixture", Path: "/unreviewed/cli"}, {Installed: true, Managed: true, Version: "0.21.1-ivoai.1", Path: filepath.Join(t.TempDir(), "graft-runtime")}} {
		graft, code := d.codeIntelligence(context.Background(), config.State{Components: map[string]config.ComponentState{"graft": state}}, []components.Spec{spec}, sandboxPrerequisites{LandlockABI: 1, Seccomp: true})
		if graft.Healthy || code.Component.Available || code.Integrity == "verified" {
			t.Fatalf("unverified runtime accepted: %+v", code)
		}
	}
}

type nodeOnlyRunner struct {
	t     *testing.T
	node  string
	fail  bool
	calls int
}

func (r *nodeOnlyRunner) LookPath(string) (string, error) {
	r.t.Fatal("ambient executable lookup")
	return "", nil
}
func (r *nodeOnlyRunner) Run(_ context.Context, path string, args []string, options platform.RunOptions) (platform.Result, error) {
	r.calls++
	if path != r.node || !reflect.DeepEqual(args, []string{"--version"}) || !options.CleanEnv || options.Timeout > 10*time.Second {
		r.t.Fatalf("unexpected execution %s %v %+v", path, args, options)
	}
	if r.fail {
		return platform.Result{Stderr: "node: libatomic.so.1: private-error-body", ExitCode: 127}, errors.New("private-error-body")
	}
	return platform.Result{Stdout: "v26.11.1\n"}, nil
}
func TestCodeIntelligenceDoctorChecksIntegrityAndOnlyPrivateNode(t *testing.T) {
	root := t.TempDir()
	d := Doctor{Store: config.NewStore(config.Paths{DataDir: root})}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	payload := "#!/bin/sh\nexit 73\n"
	if err := tw.WriteHeader(&tar.Header{Name: "graft-runtime", Mode: 0700, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(payload))
	tw.Close()
	gz.Close()
	digest := sha256.Sum256(archive.Bytes())
	spec := components.Spec{Name: "graft", Version: "0.21.1-ivoai.1", Revision: strings.Repeat("a", 64), PayloadFormat: "tar_gzip", PayloadPath: "graft-runtime", License: "MIT", SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "checksum_only", Assets: map[string]components.Asset{runtime.GOOS + "/" + runtime.GOARCH: {URL: "https://example.invalid/graft.tar.gz", SHA256: fmt.Sprintf("%x", digest)}}}
	source, err := components.ResolvedSource(spec)
	if err != nil {
		t.Fatal(err)
	}
	accept := supplychain.ValidatorFunc(func(context.Context, supplychain.ResolvedSource, string) error { return nil })
	manager := supplychain.Manager{Root: filepath.Join(root, "supply-chain"), Structural: accept, Policy: accept, Health: accept}
	staged, err := manager.StageArchive(context.Background(), source, bytes.NewReader(archive.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Promote(staged); err != nil {
		t.Fatal(err)
	}
	_, dist, err := manager.Active("graft")
	if err != nil {
		t.Fatal(err)
	}
	state := config.State{Components: map[string]config.ComponentState{"graft": {Installed: true, Managed: true, Version: spec.Version, Path: filepath.Join(dist, "graft-runtime")}}}
	node := components.ManagedNodePath(root)
	os.MkdirAll(filepath.Dir(node), 0700)
	os.WriteFile(node, []byte("fixture"), 0700)
	runner := &nodeOnlyRunner{t: t, node: node}
	d.Runner = runner
	prerequisites := sandboxPrerequisites{LandlockABI: 1, Seccomp: true, Unprivileged: true}
	graft, code := d.codeIntelligence(context.Background(), state, []components.Spec{spec}, prerequisites)
	if graft.Healthy || code.Component.Health != core.HealthUnknown || !code.Component.Available || code.Integrity != "verified" || code.Coverage != "unknown" || code.Node != "available" || code.NodeIntegrity != "not_exposed" || code.Component.Compatibility.State != core.CompatibilityUnknown {
		t.Fatalf("invalid diagnostic %+v %+v", graft, code)
	}
	if graft.Revision != spec.Revision || graft.TrustLevel != "checksum_only" || code.ArtifactSHA256 != source.Integrity.Digest {
		t.Fatal("missing verified provenance")
	}
	runner.fail = true
	_, code = d.codeIntelligence(context.Background(), state, []components.Spec{spec}, prerequisites)
	if code.Component.Available || code.Libatomic != "unavailable" {
		t.Fatalf("missing loader failure %+v", code)
	}
	body, _ := json.Marshal(code)
	if strings.Contains(string(body), "private-error-body") {
		t.Fatal("raw dependency error exposed")
	}
	runner.fail = false
	prerequisites.Seccomp = false
	_, code = d.codeIntelligence(context.Background(), state, []components.Spec{spec}, prerequisites)
	if code.Component.Available || code.Component.Compatibility.State != core.CompatibilityIncompatible {
		t.Fatal("missing sandbox prerequisite accepted")
	}
	calls := runner.calls
	os.WriteFile(filepath.Join(dist, "graft-runtime"), []byte("corrupt"), 0700)
	_, code = d.codeIntelligence(context.Background(), state, []components.Spec{spec}, prerequisites)
	if code.Integrity != "failed" || code.Component.Available || runner.calls != calls {
		t.Fatal("corrupt artifact reached runtime probe")
	}
}
