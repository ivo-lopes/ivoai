package servercmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/server"
)

type connectorRestartController struct {
	server.Controller
	err   error
	calls [][]string
}

func (c *connectorRestartController) Restart(_ context.Context, names []string) error {
	c.calls = append(c.calls, append([]string(nil), names...))
	return c.err
}

func TestConnectorAddReportsRestartFailureWithoutLosingRegistry(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "restart_succeeds", true: "restart_fails"}[failure], func(t *testing.T) {
			t.Setenv("IVOAI_TEST_MODE", "1")
			layout := server.DefaultLayout(t.TempDir())
			if err := os.MkdirAll(layout.ConfigDir, 0o700); err != nil {
				t.Fatal(err)
			}
			existing := connectorRecord{Name: "existing", Kind: "filesystem", Path: "/synthetic/existing"}
			if err := saveConnectors(layout, []connectorRecord{existing}); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(t.TempDir(), "documents")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			controller := &connectorRestartController{}
			if failure {
				controller.err = errors.New("synthetic systemctl output: API_KEY=private-fixture")
			}
			var output bytes.Buffer
			r := &runner{out: &output, errOut: &output}
			manager := server.Manager{Layout: layout, Controller: controller}
			args := []string{"add", "--name", "new-docs", "--type", "filesystem", "--path", source}
			err := r.connector(context.Background(), layout, manager, args)
			if failure {
				if err == nil || !strings.Contains(err.Error(), "connector registry saved") || !strings.Contains(err.Error(), "services did not restart") || !strings.Contains(err.Error(), "ingestion is not confirmed") {
					t.Fatal("failed restart did not report the persisted-registry/unknown-ingestion state")
				}
				if strings.Contains(err.Error(), "private-fixture") || strings.Contains(err.Error(), "API_KEY") || strings.Contains(output.String(), "private-fixture") {
					t.Fatal("private restart output was exposed")
				}
			} else if err != nil {
				t.Fatalf("successful restart: %v", err)
			}
			if !reflect.DeepEqual(controller.calls, [][]string{{"ivoai-context.service", "ivoai-gateway.service"}}) {
				t.Fatal("restart was skipped, retried, or targeted unrelated services")
			}
			records, err := loadConnectors(layout)
			if err != nil || !reflect.DeepEqual(records, []connectorRecord{existing, {Name: "new-docs", Kind: "filesystem", Path: source}}) {
				t.Fatal("new or existing connector was lost after restart outcome")
			}
			if info, err := os.Stat(connectorPath(layout)); err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("connector registry lost its private permissions")
			}
			before, err := os.ReadFile(connectorPath(layout))
			if err != nil {
				t.Fatal(err)
			}
			if err := r.connector(context.Background(), layout, manager, args); err == nil {
				t.Fatal("duplicate connector silently registered again")
			}
			after, err := os.ReadFile(connectorPath(layout))
			if err != nil || !bytes.Equal(before, after) || len(controller.calls) != 1 {
				t.Fatal("duplicate add changed persisted state or retried service activation")
			}
		})
	}
}
