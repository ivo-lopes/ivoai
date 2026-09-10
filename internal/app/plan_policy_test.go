package app

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPlanApprovalDefaultAndPersistentImmediate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	a, err := New("fixture", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.Store.Load()
	if err != nil || c.Orchestration.Auto.ResolvedPlanExecution() != "approve" {
		t.Fatal("unsafe default")
	}
	if err = a.ConfigSet("orchestration.auto.plan_execution", "immediate"); err != nil {
		t.Fatal(err)
	}
	c, err = a.Store.Load()
	if err != nil || c.Orchestration.Auto.ResolvedPlanExecution() != "immediate" {
		t.Fatal("immediate lost on reload")
	}
	before, err := os.ReadFile(a.Store.Paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.ConfigSet("orchestration.auto.plan_execution", "disable-validation"); err == nil {
		t.Fatal("invalid policy accepted")
	}
	after, err := os.ReadFile(a.Store.Paths.Config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("invalid update changed config")
	}
	if err = a.ConfigSet("orchestration.auto.plan_execution", "approve"); err != nil {
		t.Fatal(err)
	}
}
