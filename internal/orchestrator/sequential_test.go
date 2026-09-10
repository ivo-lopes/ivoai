package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/workers"
)

func TestNativeNonGitWriterFallsBackToReadOnlyPatch(t *testing.T) {
	root := t.TempDir()
	store, id := automaticBridgeSession(t, t.TempDir())
	task := routing.Task{TaskInput: routing.TaskInput{ID: "feature", Role: "implementation", Task: "implement feature", WritePaths: []string{"feature.txt"}, Acceptance: []string{"feature file contains implemented"}}}
	s := &Server{Store: store, SessionID: id, Directory: root, WorktreeRoot: t.TempDir(), plans: map[string]*runtimePlan{"plan": {Tasks: map[string]*runtimeTask{"feature": {Task: task}}}}}
	request, collect, err := s.prepareNativeRequest(context.Background(), "plan", "worker_fixture", task, workers.Request{Directory: root, Task: task.Task})
	if err != nil {
		t.Fatal(err)
	}
	defer request.Release()
	write, _ := request.Access.Metadata()
	if !request.PatchOnly || request.Access == nil || write {
		t.Fatal("fallback granted direct filesystem writes")
	}
	patch := "diff --git a/feature.txt b/feature.txt\nnew file mode 100644\n--- /dev/null\n+++ b/feature.txt\n@@ -0,0 +1 @@\n+implemented\n"
	if err := collect(workers.Result{Text: patch}); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(root, "feature.txt")); string(body) != "implemented\n" {
		t.Fatal("sequential result missing")
	}
	v, _ := store.Get(id)
	if !v.ParallelWriteDegraded {
		t.Fatal("degraded mode invisible")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatal("silent git init")
	}
}
