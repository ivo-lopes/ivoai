package benchmarkfixtures

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func editFixture(t *testing.T, id string) (string, EditSnapshot) {
	t.Helper()
	s, ok := Lookup(id)
	if !ok || s.Execution != "workspace_edit" {
		t.Fatal("edit fixture missing")
	}
	root := t.TempDir()
	if err := Materialize(root, s.Fixture); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git/config"), []byte("protected metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := SnapshotEditTree(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, before
}

func applyReviewedEdit(t *testing.T, id, root string) EditSnapshot {
	t.Helper()
	for _, name := range EditPaths(id) {
		path := filepath.Join(root, name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if id == "small_known_file_correction" {
			body = []byte(strings.Replace(string(body), "if value > max { return min }", "if value > max { return max }", 1))
		} else {
			body = []byte(strings.ReplaceAll(string(body), "discount", "applyDiscount"))
			// Import filenames remain discount.ts; only identifiers are renamed.
			body = []byte(strings.ReplaceAll(string(body), "/applyDiscount.ts", "/discount.ts"))
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	after, err := SnapshotEditTree(root)
	if err != nil {
		t.Fatal(err)
	}
	return after
}

func TestEditContractsRejectUnauthorisedChanges(t *testing.T) {
	for _, id := range []string{"small_known_file_correction", "cross_module_refactor"} {
		t.Run(id, func(t *testing.T) {
			if ValidateEdit(id, EditSnapshot{}, EditSnapshot{}) {
				t.Fatal("missing edit source accepted")
			}
			root, before := editFixture(t, id)
			if ValidateEdit(id, before, before) {
				t.Fatal("answer without edit accepted")
			}
			after := applyReviewedEdit(t, id, root)
			if !ValidateEdit(id, before, after) {
				t.Fatal("reviewed edit rejected")
			}
			for _, mutation := range []string{"extra", "metadata", "permission", "directory", "oracle"} {
				t.Run(mutation, func(t *testing.T) {
					other, baseline := editFixture(t, id)
					applyReviewedEdit(t, id, other)
					switch mutation {
					case "extra":
						os.WriteFile(filepath.Join(other, "unexpected"), []byte("extra"), 0600)
					case "metadata":
						os.WriteFile(filepath.Join(other, ".git/config"), []byte("changed"), 0600)
					case "permission":
						os.Chmod(filepath.Join(other, EditPaths(id)[0]), 0777)
					case "directory":
						os.Mkdir(filepath.Join(other, "extra-dir"), 0700)
					case "oracle":
						name := "go.mod"
						if id == "cross_module_refactor" {
							name = "package.json"
						}
						os.WriteFile(filepath.Join(other, name), []byte("tampered"), 0600)
					}
					changed, err := SnapshotEditTree(other)
					if err == nil && ValidateEdit(id, baseline, changed) {
						t.Fatal("mutation outside edit contract accepted")
					}
				})
			}
		})
	}
}

func TestPureEditShapeRefusesExecutableInjection(t *testing.T) {
	for _, body := range []string{
		`package mathpkg; import "os"; func Clamp(value,min,max int)int{os.Exit(0);return value}`,
		`package mathpkg; func init(){}; func Clamp(value,min,max int)int{return value}`,
		`package mathpkg; func Clamp(value,min,max int)int{return Clamp(value,min,max)}`,
		`package mathpkg; func Clamp(value,min,max int)int{for{};return value}`,
		`package mathpkg; func Clamp(value,min,max int)int{go func(){}();return value}`,
		`package mathpkg; func Clamp(value,min,max int)int{if (func()bool{for{};return true})() || !false{return value};return min}`,
	} {
		if pureClamp([]byte(body)) {
			t.Fatal("unsafe generated Go shape accepted")
		}
	}
	original := []byte("export function discount(amount: number): number { return amount * 9 / 10; }\n")
	if !identifierOnlyRename(original, []byte("export function applyDiscount(amount: number): number {\n return amount * 9 / 10; } // harmless comment\n")) {
		t.Fatal("whitespace/comment compatible rename rejected")
	}
	for _, extra := range []string{"process.exit(0);", "await import('node:fs');", "export const discount=applyDiscount;"} {
		if identifierOnlyRename(original, append(bytes.ReplaceAll(original, []byte("discount"), []byte("applyDiscount")), []byte(extra)...)) {
			t.Fatal("extra TS token accepted")
		}
	}
}

func TestEditSnapshotAndOutputAreBounded(t *testing.T) {
	root, _ := editFixture(t, "small_known_file_correction")
	if err := unix.Mkfifo(filepath.Join(root, "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := SnapshotEditTree(root); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO snapshot blocked")
	}
	out := &boundedOracleOutput{limit: 17}
	io.Copy(out, strings.NewReader(strings.Repeat("x", 10000)))
	if !out.overflow || out.Len() != 17 {
		t.Fatal("live output bound bypassed")
	}
}

// NOT_RUN unless the host supplies existing reviewed sandbox/toolchain inputs.
// This executes only reviewed synthetic edits, never an agent or inference.
func TestBenchmarkSandboxedEditOracles(t *testing.T) {
	if os.Getenv("IVOAI_BENCHMARK_EDIT_ORACLES") != "1" {
		t.Skip("NOT_RUN: explicit reviewed Go/Node/bwrap inputs required")
	}
	tools := OracleTools{Go: os.Getenv("IVOAI_BENCHMARK_ORACLE_GO"), Node: os.Getenv("IVOAI_GRAFT_TEST_NODE"), Bwrap: os.Getenv("IVOAI_BENCHMARK_BWRAP")}
	for _, id := range []string{"small_known_file_correction", "cross_module_refactor"} {
		t.Run(id, func(t *testing.T) {
			root, before := editFixture(t, id)
			identity, err := OracleIdentity(id, tools)
			if err != nil || len(identity) != 64 || ProbeEditOracle(context.Background(), id, root, tools) != nil {
				t.Fatal("required edit oracle containment unavailable")
			}
			after := applyReviewedEdit(t, id, root)
			if err := RunEditOracle(context.Background(), id, before, after, tools); err != nil {
				t.Fatal("reviewed edit executable oracle failed")
			}
			if id == "small_known_file_correction" {
				os.WriteFile(filepath.Join(root, EditPaths(id)[0]), []byte("package mathpkg; func Clamp(value,min,max int) int{return value}\n"), 0600)
				wrong, _ := SnapshotEditTree(root)
				if !ValidateEdit(id, before, wrong) || RunEditOracle(context.Background(), id, before, wrong, tools) == nil {
					t.Fatal("wrong pure Go implementation bypassed executable tests")
				}
			}
			t.Logf("offline_edit_oracle=VERIFIED mode=%s provider_attempts=0", id)
		})
	}
}
