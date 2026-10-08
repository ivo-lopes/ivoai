package platform

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecRunnerReportsSignal(t *testing.T) {
	for _, tc := range []struct{ signal, message string }{
		{"ILL", "illegal instruction"},
		{"TERM", "terminated"},
	} {
		t.Run(tc.signal, func(t *testing.T) {
			result, err := (ExecRunner{}).Run(context.Background(), "/bin/sh", []string{"-c", "ulimit -c 0; kill -" + tc.signal + " $$"}, RunOptions{Timeout: time.Second})
			if result.ExitCode != -1 || err == nil || !strings.Contains(err.Error(), "signal: "+tc.message) {
				t.Fatalf("missing signal diagnosis: result=%+v err=%v", result, err)
			}
		})
	}
}

func TestExecRunnerPreservesNumericExitStatus(t *testing.T) {
	result, err := (ExecRunner{}).Run(context.Background(), "/bin/sh", []string{"-c", "exit 7"}, RunOptions{Timeout: time.Second})
	if result.ExitCode != 7 || err == nil || !strings.Contains(err.Error(), "exited with status 7") {
		t.Fatalf("numeric exit status changed: result=%+v err=%v", result, err)
	}
}
