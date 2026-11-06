package servercmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/server"
)

type privateDockerUpgradeController struct {
	t                                  *testing.T
	layout                             server.Layout
	prefix, credentials, probe, helper string
}

func (c *privateDockerUpgradeController) DaemonReload(context.Context) error     { return nil }
func (c *privateDockerUpgradeController) Enable(context.Context, []string) error { return nil }
func (c *privateDockerUpgradeController) Logs(context.Context, string, int) (string, error) {
	return "", errors.New("systemd is outside the private Docker fixture")
}
func (c *privateDockerUpgradeController) Restart(ctx context.Context, _ []string) error {
	if err := c.Stop(ctx, nil); err != nil {
		return err
	}
	return c.Start(ctx, nil)
}
func (c *privateDockerUpgradeController) Stop(ctx context.Context, _ []string) error {
	for _, component := range []string{"memory", "qdrant"} {
		cmd := exec.CommandContext(ctx, "docker", "ps", "--filter", "name=^"+c.prefix+"-"+component+"$", "--format", "{{.ID}}")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return err
		}
		if id := strings.TrimSpace(out.String()); id != "" {
			if err := runUpgradeDocker(ctx, "stop", "--time", "10", id); err != nil {
				return err
			}
		}
	}
	return nil
}
func (c *privateDockerUpgradeController) Status(ctx context.Context, _ []string) ([]server.ServiceState, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "--filter", "label=ivoai-private-upgrade="+c.prefix, "--format", "{{.ID}}")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	active := strings.TrimSpace(out.String()) != ""
	states := []server.ServiceState{}
	for _, name := range server.ManagedServices {
		states = append(states, server.ServiceState{Name: name, Active: name == "ivoai-dependencies.service" && active})
	}
	return states, nil
}
func (c *privateDockerUpgradeController) Start(ctx context.Context, _ []string) error {
	body, err := os.ReadFile(filepath.Join(c.layout.ConfigDir, "compose.yaml"))
	if err != nil {
		return err
	}
	images := map[string]string{}
	component := ""
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			component = strings.TrimSuffix(strings.TrimSpace(line), ":")
		}
		if strings.HasPrefix(strings.TrimSpace(line), "image:") {
			images[component] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
		}
	}
	for _, mount := range []string{c.layout.MemoryDir, c.layout.QdrantDir, c.layout.QdrantInitDir, c.layout.QdrantSnapshotsDir} {
		if err := secureChownTree(mount, 1000, 1000); err != nil {
			return err
		}
	}
	common := []string{"run", "--rm", "--detach", "--network", "none", "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--ulimit", "nproc=512:512", "--ulimit", "nofile=65536:65536", "--label", "ivoai-private-upgrade=" + c.prefix, "--env-file", c.credentials, "--mount", "type=bind,src=" + c.probe + ",dst=/ivoai-probe,readonly", "--mount", "type=bind,src=" + c.helper + ",dst=/ivoai-test-helper,readonly", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m"}
	memory := append(append([]string{}, common...), "--name", c.prefix+"-memory", "--mount", "type=bind,src="+c.layout.MemoryDir+",dst=/data", "--entrypoint", "/usr/local/bin/ai-memory", images["ai-memory"], "--data-dir", "/data", "serve", "--transport", "http", "--bind", "127.0.0.1:49374", "--no-watcher")
	if err := runUpgradeDocker(ctx, memory...); err != nil {
		return err
	}
	qdrant := append(append([]string{}, common...), "--name", c.prefix+"-qdrant", "--mount", "type=bind,src="+c.layout.QdrantDir+",dst=/qdrant/storage", "--mount", "type=bind,src="+c.layout.QdrantInitDir+",dst=/qdrant/init", "--mount", "type=bind,src="+c.layout.QdrantSnapshotsDir+",dst=/qdrant/snapshots", images["qdrant"])
	if err := runUpgradeDocker(ctx, qdrant...); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if runUpgradeDocker(ctx, "exec", c.prefix+"-memory", "/ivoai-probe", "server", "internal-memory-upgrade-probe") == nil && runUpgradeDocker(ctx, "exec", c.prefix+"-qdrant", "/ivoai-probe", "server", "internal-qdrant-upgrade-probe") == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("fixture runtimes did not become authenticated/ready")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (c *privateDockerUpgradeController) resourceObservation(ctx context.Context) map[string]string {
	out := map[string]string{}
	for _, component := range []string{"memory", "qdrant"} {
		command := exec.CommandContext(ctx, "docker", "exec", "--env", "IVOAI_UPGRADE_HTTP_FIXTURE=resources", c.prefix+"-"+component, "/ivoai-test-helper", "-test.run", "^TestPrivateUpgradeHTTPFixtureHelper$")
		var body bytes.Buffer
		command.Stdout = &body
		command.Stderr = io.Discard
		out[component] = "unavailable; not zero"
		if err := command.Run(); err == nil {
			for _, line := range strings.Split(body.String(), "\n") {
				if strings.HasPrefix(line, "IVOAI_RESOURCE_OBSERVATION ") {
					out[component] = strings.TrimPrefix(line, "IVOAI_RESOURCE_OBSERVATION ")
				}
			}
		}
	}
	return out
}

func TestRealPrivateDockerDependencyUpgradeAndCoordinatedRollback(t *testing.T) {
	if os.Getenv("IVOAI_TEST_PRIVATE_DOCKER_UPGRADE") != "1" {
		t.Skip("requires an explicitly audited disposable Docker socket, namespace and reviewed old memory store")
	}
	if os.Geteuid() != 0 {
		t.Fatal("run in the prepared mapped-root namespace to preserve real service UID1000")
	}
	socket := os.Getenv("IVOAI_TEST_PRIVATE_DOCKER_SOCKET")
	expectedRoot := os.Getenv("IVOAI_TEST_PRIVATE_DOCKER_ROOT")
	probe := os.Getenv("IVOAI_TEST_UPGRADE_PROBE")
	source := os.Getenv("IVOAI_TEST_MEMORY_V2")
	if !filepath.IsAbs(socket) || os.Getenv("DOCKER_HOST") != "unix://"+socket || socket == "/var/run/docker.sock" || expectedRoot == "" || probe == "" || source == "" {
		t.Fatal("explicit private Docker/store/probe boundary required")
	}
	cmd := exec.Command("docker", "info", "--format", "{{.DockerRootDir}} {{.CgroupDriver}}")
	info, err := cmd.Output()
	if err != nil || !strings.HasPrefix(string(info), expectedRoot+" ") {
		t.Fatal("unexpected Docker data root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	layout := server.DefaultLayout(t.TempDir())
	manager := server.Manager{Layout: layout, Architecture: "amd64", ContainerUser: "1000:1000"}
	if err := manager.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(layout.MemoryDir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	old := strings.ReplaceAll(strings.ReplaceAll(server.ComposeYAML, server.MemoryImageReference(), previousMemoryImage), server.QdrantImageReference(), previousQdrantImage)
	if err := os.WriteFile(filepath.Join(layout.ConfigDir, "compose.yaml"), []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	var entropy [24]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(entropy[:])
	prefix := "ivoai-upgrade-real-" + token[:12]
	credentials := filepath.Join(layout.SecretsDir, "fixture.env")
	if err := os.WriteFile(credentials, []byte("AI_MEMORY_AUTH_TOKEN="+token+"\nAI_MEMORY_EMBEDDING_PROVIDER=none\nQDRANT__SERVICE__API_KEY="+token+"\nQDRANT_INIT_FILE_PATH=/qdrant/init/.qdrant-initialized\n"), 0600); err != nil {
		t.Fatal(err)
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	controller := &privateDockerUpgradeController{t: t, layout: layout, prefix: prefix, credentials: credentials, probe: probe, helper: helper}
	manager.Controller = controller
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = controller.Stop(cleanup, nil)
	})
	if err := controller.Start(ctx, nil); err != nil {
		t.Fatal(err)
	}
	observations := map[string]map[string]string{"baseline_after_ready": controller.resourceObservation(ctx)}
	if err := runUpgradeDocker(ctx, "exec", "--env", "IVOAI_UPGRADE_HTTP_FIXTURE=seed", prefix+"-qdrant", "/ivoai-test-helper", "-test.run", "^TestPrivateUpgradeHTTPFixtureHelper$"); err != nil {
		t.Fatal("seed official old Qdrant", err)
	}
	if err := runUpgradeDocker(ctx, "exec", "--env", "IVOAI_UPGRADE_HTTP_FIXTURE=memory", prefix+"-memory", "/ivoai-test-helper", "-test.run", "^TestPrivateUpgradeHTTPFixtureHelper$"); err != nil {
		t.Fatal("read official old memory", err)
	}
	r := &runner{out: io.Discard, errOut: io.Discard}
	archive := filepath.Join(layout.BackupDir, "before.tar.gz")
	var receipt string
	if err := server.WithDependencyMutationLock(ctx, layout, func(locked context.Context) error {
		return r.withQuiescedServices(locked, manager, func() error {
			var err error
			receipt, err = manager.UpgradeDependencies(locked, archive, newDockerUpgradeValidator(probe, "1000:1000"))
			return err
		}, false)
	}); err != nil {
		t.Fatal("actual candidate/activation failed", err)
	}
	for _, pair := range []struct{ component, mode string }{{"memory", "memory"}, {"qdrant", "verify"}} {
		if err := runUpgradeDocker(ctx, "exec", "--env", "IVOAI_UPGRADE_HTTP_FIXTURE="+pair.mode, prefix+"-"+pair.component, "/ivoai-test-helper", "-test.run", "^TestPrivateUpgradeHTTPFixtureHelper$"); err != nil {
			t.Fatal("candidate lost exact stored fixture", pair.component, err)
		}
	}
	observations["candidate_after_exact_reads"] = controller.resourceObservation(ctx)
	if err := server.WithDependencyMutationLock(ctx, layout, func(locked context.Context) error {
		return r.withQuiescedServices(locked, manager, func() error {
			// Create sidecars only after the real forward runtime has stopped.
			// They must never be replayed against the restored old database.
			expected := server.DefaultLayout(t.TempDir())
			if err := server.Restore(expected, archive); err != nil {
				return err
			}
			projection := privateMemoryProjection(t, expected.MemoryDir)
			extras := map[string]string{"rollback-only.db-wal": "forward-only-wal", "rollback-only.db-shm": "forward-only-shm", "v3extras/rollback-only": "forward-only-extra"}
			for name, body := range extras {
				path := filepath.Join(layout.MemoryDir, name)
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					return errors.New("post-upgrade fixture already existed")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					return err
				}
			}
			if err := manager.RollbackDependencies(locked, receipt); err != nil {
				return err
			}
			if !reflect.DeepEqual(projection, privateMemoryProjection(t, layout.MemoryDir)) {
				return errors.New("active Memory differs from the exact backup tree before old runtime restart")
			}
			quarantines, err := filepath.Glob(filepath.Join(filepath.Dir(layout.MemoryDir), "post-upgrade-memory-*", "memory"))
			if err != nil || len(quarantines) != 1 {
				return errors.New("forward Memory quarantine missing")
			}
			for name, body := range extras {
				got, err := os.ReadFile(filepath.Join(quarantines[0], name))
				if err != nil || string(got) != body {
					return errors.New("forward-only Memory artifact not preserved in quarantine")
				}
			}
			return nil
		}, false)
	}); err != nil {
		t.Fatal("actual coordinated rollback failed", err)
	}
	for _, pair := range []struct{ component, mode string }{{"memory", "memory"}, {"qdrant", "verify"}} {
		if err := runUpgradeDocker(ctx, "exec", "--env", "IVOAI_UPGRADE_HTTP_FIXTURE="+pair.mode, prefix+"-"+pair.component, "/ivoai-test-helper", "-test.run", "^TestPrivateUpgradeHTTPFixtureHelper$"); err != nil {
			t.Fatal("rollback lost exact stored fixture", pair.component, err)
		}
	}
	observations["rollback_after_exact_reads"] = controller.resourceObservation(ctx)
	report := map[string]any{"pass": true, "actual_official_images": true, "ai_memory_versions": []string{"2.6.3", "3.4.2", "2.6.3"}, "qdrant_versions": []string{"1.19.2", "1.20.0", "1.19.2"}, "core_backup_restore": true, "original_index_preserved": true, "exact_backup_memory_tree_before_restart": true, "forward_only_wal_shm_extras_quarantined": true, "probe_scope": "known authenticated read/status/query/catalog and vector contracts only; not a universal tool inventory or full provider parity", "stage_validator_actual_startup": true, "authenticated_read_and_401": true, "exact_memory_fixture_read": true, "exact_vector_point_read": true, "service_uid": 1000, "service_gid": 1000, "private_docker_info": strings.TrimSpace(string(info)), "systemd_certified": false, "production_accessed": false, "cgroup_limits_applied": false, "resource_observations": observations, "resource_observation_scope": "guest procfs for the unique runtime process: observed RSS KiB and process CPU ticks after readiness/read; not peak, normalized comparison or session cost"}
	if path := os.Getenv("IVOAI_TEST_UPGRADE_REPORT"); path != "" {
		body, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(path, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log("actual official image upgrade+Core backup/stage+data/index/pin rollback PASS; mapped service UID/GID1000; systemd/production not certified;", strings.TrimSpace(string(info)))
}

// privateMemoryProjection compares exact restored names and file bytes before
// restarting the old runtime, which may legitimately create fresh SQLite WALs.
func privateMemoryProjection(t *testing.T, root string) map[string]string {
	t.Helper()
	projection := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			projection[rel] = "directory"
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("unsupported memory projection entry")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		projection[rel] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func TestPrivateUpgradeHTTPFixtureHelper(t *testing.T) {
	mode := os.Getenv("IVOAI_UPGRADE_HTTP_FIXTURE")
	if mode == "" {
		t.Skip("container-only fixed fixture helper, not a certification scenario")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	request := func(method, url string, body []byte, key string) []byte {
		req, err := http.NewRequest(method, url, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if strings.Contains(url, ":49374") {
			req.Header.Set("Authorization", "Bearer "+key)
		} else {
			req.Header.Set("api-key", key)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if err != nil || response.StatusCode != 200 {
			t.Fatal("fixture endpoint failed", response.StatusCode, err)
		}
		return data
	}
	switch mode {
	case "resources":
		processes, err := os.ReadDir("/proc")
		if err != nil {
			t.Fatal(err)
		}
		pid := ""
		for _, entry := range processes {
			if !entry.IsDir() {
				continue
			}
			if _, err := strconv.Atoi(entry.Name()); err != nil {
				continue
			}
			comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
			if err != nil {
				continue
			}
			name := strings.TrimSpace(string(comm))
			if name == "ai-memory" || name == "qdrant" {
				if pid != "" {
					t.Fatal("ambiguous runtime process identity")
				}
				pid = entry.Name()
			}
		}
		if pid == "" {
			t.Fatal("actual runtime process not found")
		}
		raw, err := os.ReadFile(filepath.Join("/proc", pid, "status"))
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		for _, line := range strings.Split(string(raw), "\n") {
			pieces := strings.Fields(line)
			if len(pieces) >= 2 {
				fields[strings.TrimSuffix(pieces[0], ":")] = pieces[1]
			}
		}
		if fields["Name"] != "ai-memory" && fields["Name"] != "qdrant" {
			t.Fatal("resource source is not the actual runtime PID1")
		}
		stat, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
		if err != nil {
			t.Fatal(err)
		}
		end := strings.LastIndex(string(stat), ")")
		if end < 0 {
			t.Fatal("invalid process stat")
		}
		tokens := strings.Fields(string(stat)[end+1:])
		if len(tokens) < 20 {
			t.Fatal("incomplete process CPU state")
		}
		value := map[string]string{"source": "runtime_process_procfs", "pid": pid, "process": fields["Name"], "rss_kib": fields["VmRSS"], "user_cpu_ticks": tokens[11], "system_cpu_ticks": tokens[12], "start_ticks": tokens[19]}
		for _, key := range []string{"rss_kib", "user_cpu_ticks", "system_cpu_ticks", "start_ticks"} {
			if _, err := strconv.ParseUint(value[key], 10, 64); err != nil {
				t.Fatal("invalid numeric resource observation", key)
			}
		}
		encoded, _ := json.Marshal(value)
		fmt.Println("IVOAI_RESOURCE_OBSERVATION " + string(encoded))
	case "seed":
		key := os.Getenv("QDRANT__SERVICE__API_KEY")
		request("PUT", "http://127.0.0.1:6333/collections/upgrade_fixture", []byte(`{"vectors":{"size":3,"distance":"Cosine"}}`), key)
		request("PUT", "http://127.0.0.1:6333/collections/upgrade_fixture/points?wait=true", []byte(`{"points":[{"id":1,"vector":[1,0,0],"payload":{"marker":"coordinated-upgrade-20261104"}}]}`), key)
	case "verify":
		data := request("GET", "http://127.0.0.1:6333/collections/upgrade_fixture/points/1?with_payload=true&with_vector=true", nil, os.Getenv("QDRANT__SERVICE__API_KEY"))
		var result struct {
			Result struct {
				ID      int               `json:"id"`
				Vector  []float64         `json:"vector"`
				Payload map[string]string `json:"payload"`
			} `json:"result"`
		}
		if json.Unmarshal(data, &result) != nil || result.Result.ID != 1 || len(result.Result.Vector) != 3 || result.Result.Vector[0] != 1 || result.Result.Payload["marker"] != "coordinated-upgrade-20261104" {
			t.Fatal("exact vector fixture changed")
		}
	case "memory":
		data := request("POST", "http://127.0.0.1:49374/mcp", []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read_page","arguments":{"workspace":"fixture","project":"ivoai","path":"decisions/compatibility"}}}`), os.Getenv("AI_MEMORY_AUTH_TOKEN"))
		if !bytes.Contains(data, []byte("upgrade-marker-20261104")) {
			t.Fatal("exact memory fixture missing")
		}
	default:
		t.Fatal(fmt.Sprintf("unsupported private fixture mode %q", mode))
	}
}

func TestDependencyUpgradeRequiresActualResourceLimitSupport(t *testing.T) {
	for _, body := range []string{`{"driver":"none","memory":true,"cpu":true,"pids":true}`, `{"driver":"systemd","memory":false,"cpu":true,"pids":true}`, `{"driver":"cgroupfs","memory":true,"cpu":false,"pids":true}`, `{"driver":"systemd","memory":true,"cpu":true,"pids":false}`, `invalid`} {
		if dockerResourceIsolationSupported([]byte(body)) {
			t.Fatal("unsupported resource boundary admitted")
		}
	}
	if !dockerResourceIsolationSupported([]byte(`{"driver":"systemd","memory":true,"cpu":true,"pids":true}`)) {
		t.Fatal("legitimate resource isolation rejected")
	}
}
