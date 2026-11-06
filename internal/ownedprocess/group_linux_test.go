//go:build linux

package ownedprocess

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestSignalsRetireBeforeLeaderReaping(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	group, err := New(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if err = group.Wait(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if err = group.Signal(syscall.SIGTERM); !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("late signal was not retired: %v", err)
		}
	}
	if group.Forced() {
		t.Fatal("normal completion was marked forced")
	}
}

func TestUnownedGroupIsRejected(t *testing.T) {
	if _, err := New(&exec.Cmd{}); err == nil {
		t.Fatal("unowned group admitted")
	}
}
