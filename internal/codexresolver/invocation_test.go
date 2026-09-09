package codexresolver

import (
	"reflect"
	"testing"
)

func TestExecInvocationSeparatesScopesAndPreservesValues(t *testing.T) {
	input := []string{"-c", "mcp_servers.ivoai-memory.url=\"http://127.0.0.1:123/mcp\"", "-a", "never", "--sandbox", "workspace-write", "--skip-git-repo-check", "--color", "never", "--add-dir", "--skip-git-repo-check"}
	v := SplitExecArguments(input)
	if !reflect.DeepEqual(v.ExecArgs, []string{"--skip-git-repo-check", "--color", "never"}) || !reflect.DeepEqual(v.GlobalArgs, append(append([]string{}, input[:6]...), input[9:]...)) {
		t.Fatalf("wrong option ownership: %+v", v)
	}
	for _, resume := range []string{"", "thread_fixture"} {
		got := v.Arguments(resume, "/administration", []string{"--model", "model-fixture", "-c", "model_reasoning_effort=\"high\""})
		want := append([]string{}, input[:2]...)
		want = append(want, "-c", "model_reasoning_effort=\"high\"", "-a", "never", "--sandbox", "workspace-write", "--add-dir", "--skip-git-repo-check", "exec", "--skip-git-repo-check", "--color", "never", "-C", "/administration")
		if resume != "" {
			want = append(want, "resume")
		}
		want = append(want, "--json", "--model", "model-fixture")
		if resume != "" {
			want = append(want, resume)
		}
		want = append(want, "-")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resume=%q: got %q want %q", resume, got, want)
		}
	}
}

func TestExecInvocationSupportsAdministrativeNonGitWithoutChangingSandbox(t *testing.T) {
	v := SplitExecArguments([]string{"--sandbox", "read-only", "-a", "never"})
	got := v.Arguments("", "/root-equivalent", nil)
	want := []string{"--sandbox", "read-only", "-a", "never", "exec", "--skip-git-repo-check", "--color", "never", "-C", "/root-equivalent", "--json", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExecInvocationDoesNotDuplicateManagedFramingOrConsumeOptionValues(t *testing.T) {
	v := SplitExecArguments([]string{"--json", "--color=always", "--skip-git-repo-check", "--output-schema", "--skip-git-repo-check"})
	got := v.Arguments("", "/administration", nil)
	want := []string{"exec", "--output-schema", "--skip-git-repo-check", "--skip-git-repo-check", "--color", "never", "-C", "/administration", "--json", "-"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("framing or option value corrupted: %q", got)
	}
}
