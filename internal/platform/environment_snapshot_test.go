package platform

import (
	"context"
	"reflect"
	"testing"
)

func TestSnapshotEnvironmentMatchesExecRunner(t *testing.T) {
	t.Setenv("IVOAI_SCOPE_SNAPSHOT_FIXTURE", "inherited")
	for _, tc := range []struct {
		name    string
		options RunOptions
		want    string
	}{
		{"nil_inherits", RunOptions{}, "inherited"},
		{"empty_inherits", RunOptions{Env: []string{}}, "inherited"},
		{"override", RunOptions{Env: []string{"IVOAI_SCOPE_SNAPSHOT_FIXTURE=override"}}, "override"},
		{"clean_nil_empty", RunOptions{CleanEnv: true}, ""},
		{"clean_empty", RunOptions{CleanEnv: true, Env: []string{}}, ""},
		{"clean_override", RunOptions{CleanEnv: true, Env: []string{"IVOAI_SCOPE_SNAPSHOT_FIXTURE=clean"}}, "clean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := SnapshotEnvironment(tc.options)
			if snapshot == nil {
				t.Fatal("snapshot must distinguish explicit empty from inheritance")
			}
			args := []string{"-c", `printf '%s' "$IVOAI_SCOPE_SNAPSHOT_FIXTURE"`}
			original, err := (ExecRunner{}).Run(context.Background(), "/bin/sh", args, tc.options)
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := (ExecRunner{}).Run(context.Background(), "/bin/sh", args, RunOptions{Env: snapshot, CleanEnv: true})
			if err != nil {
				t.Fatal(err)
			}
			if original.Stdout != tc.want || frozen.Stdout != tc.want {
				t.Fatal("snapshot diverged from process environment semantics")
			}
			if len(tc.options.Env) > 0 {
				before := append([]string(nil), tc.options.Env...)
				snapshot[0] = "MUTATED=fixture"
				if !reflect.DeepEqual(before, tc.options.Env) {
					t.Fatal("snapshot aliases caller environment")
				}
			}
		})
	}
}
