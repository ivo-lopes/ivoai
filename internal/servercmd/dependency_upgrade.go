package servercmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/server"
)

func (r *runner) upgrade(ctx context.Context, layout server.Layout, manager server.Manager, args []string) error {
	fs := flag.NewFlagSet("server upgrade", flag.ContinueOnError)
	fs.SetOutput(r.errOut)
	backup := fs.String("backup-output", "", "new backup path directly under the managed backup directory")
	rollback := fs.String("rollback-receipt", "", "private receipt for explicit data and dependency rollback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || (*backup != "" && *rollback != "") {
		return errors.New("usage: server upgrade [--backup-output PATH | --rollback-receipt PATH]")
	}
	if os.Geteuid() != 0 {
		return errors.New("server dependency upgrade requires existing root authority")
	}
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return errors.New("live server upgrade unavailable in test-mode CLI; isolated runtime gate required")
	}
	identity, err := serviceUserIdentity()
	if err != nil {
		return err
	}
	manager.ContainerUser = identity
	if *rollback != "" {
		return server.WithDependencyMutationLock(ctx, layout, func(locked context.Context) error {
			operationError := r.withQuiescedServices(locked, manager, func() error {
				if err := manager.RollbackDependencies(locked, *rollback); err != nil {
					return err
				}
				if err := ensureRestoredServiceOwnership(layout); err != nil {
					return err
				}
				return ensureServiceOwnership(layout)
			}, false)
			if operationError == nil {
				operationError = waitForServicesStable(locked, 20*time.Second, 500*time.Millisecond, 2*time.Second, manager.Status)
			}
			if operationError != nil {
				recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if stopErr := manager.Stop(recoveryCtx); stopErr != nil {
					return errors.Join(operationError, errors.New("rollback service stop failed; operator inspection required"))
				}
			}
			return operationError
		})
	}
	if *backup == "" {
		*backup = filepath.Join(layout.BackupDir, "pre-dependencies-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".tar.gz")
	}
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	image := server.MemoryImageReference()
	if err := requireDockerResourceIsolation(ctx); err != nil {
		return err
	}
	// Prefetch the immutable image before stopping services. No data is mounted.
	for _, reference := range []string{image, server.QdrantImageReference()} {
		if err := runUpgradeDocker(ctx, "pull", reference); err != nil {
			return err
		}
	}
	fmt.Fprintln(r.out, "Pre-upgrade backup: "+*backup)
	var receipt string
	err = server.WithDependencyMutationLock(ctx, layout, func(locked context.Context) error {
		operationError := r.withQuiescedServices(locked, manager, func() error {
			var operationErr error
			receipt, operationErr = manager.UpgradeDependencies(locked, *backup, newDockerUpgradeValidator(binary, identity))
			if receipt != "" {
				fmt.Fprintln(r.out, "Dependency rollback receipt: "+receipt)
			}
			if operationErr != nil {
				return operationErr
			}
			return ensureServiceOwnership(layout)
		}, false)
		if operationError == nil {
			operationError = waitForServicesStable(locked, 20*time.Second, 500*time.Millisecond, 2*time.Second, manager.Status)
		}
		if operationError != nil && receipt != "" {
			recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if stopErr := manager.Stop(recoveryCtx); stopErr != nil {
				return errors.Join(operationError, errors.New("upgrade failed; service stop also failed; operator inspection required"))
			}
		}
		return operationError
	})
	if err != nil && receipt != "" {
		return fmt.Errorf("dependency upgrade failed; services left stopped, do not switch only the ai-memory image; rollback data and runtime together with server upgrade --rollback-receipt %s: %w", receipt, err)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(r.out, "Dependency upgrade completed; rollback requires the matching data backup and private receipt.")
	return nil
}

func runUpgradeDocker(ctx context.Context, args ...string) error {
	command := exec.CommandContext(ctx, "docker", args...)
	// Never echo docker output: an existing daemon or runtime may print secrets.
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("isolated dependency runtime operation failed: %w", err)
	}
	return nil
}

func newDockerDependencyValidator(probeBinary, identity, image string) server.DependencyValidator {
	return func(ctx context.Context, copyLayout server.Layout) (validationErr error) {
		if _, _, found := strings.Cut(identity, ":"); !found {
			return errors.New("invalid non-root container identity")
		}
		parts := strings.Split(identity, ":")
		if len(parts) != 2 {
			return errors.New("invalid container identity")
		}
		uid, uerr := strconv.Atoi(parts[0])
		gid, gerr := strconv.Atoi(parts[1])
		if uerr != nil || gerr != nil || uid <= 0 || gid < 0 {
			return errors.New("validator requires a non-root service identity")
		}
		beforeWiki, err := memoryWikiDigest(ctx, copyLayout.MemoryDir)
		if err != nil {
			return err
		}
		if err := secureChownTree(copyLayout.MemoryDir, uid, gid); err != nil {
			return err
		}
		probe, err := os.Open(probeBinary)
		if err != nil {
			return err
		}
		defer probe.Close()
		info, err := probe.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("upgrade probe executable unavailable")
		}
		// Copy the running trusted Core binary into the disposable tree; an update
		// of the installation cannot replace the mounted probe during validation.
		stagedBinary := filepath.Join(copyLayout.InstallDir, "ivoai-upgrade-probe")
		staged, err := os.OpenFile(stagedBinary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(staged, probe)
		if copyErr == nil {
			copyErr = staged.Chmod(0755)
		}
		closeErr := staged.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		var entropy [32]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return err
		}
		token := hex.EncodeToString(entropy[:])
		name := "ivoai-memory-validation-" + hex.EncodeToString(entropy[:8])
		credentials := filepath.Join(copyLayout.SecretsDir, "validation.env")
		if err := platform.AtomicWritePrivate([]byte("AI_MEMORY_AUTH_TOKEN="+token+"\nAI_MEMORY_EMBEDDING_PROVIDER=none\n"), credentials); err != nil {
			return err
		}
		defer os.Remove(credentials)
		cleanup := func() error {
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return runUpgradeDocker(stopCtx, "rm", "--force", name)
		}
		defer func() {
			if err := cleanup(); err != nil && validationErr == nil {
				validationErr = errors.New("candidate cleanup failed; runtime assets must not be installed")
			}
		}()
		if strings.ContainsAny(copyLayout.MemoryDir, ",\n\r") || strings.ContainsAny(stagedBinary, ",\n\r") {
			return errors.New("unsupported validation mount path")
		}
		err = runUpgradeDocker(ctx, "run", "--detach", "--name", name, "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--pids-limit", "64", "--ulimit", "nproc=512:512", "--ulimit", "nofile=65536:65536", "--memory", "512m", "--cpus", "1", "--user", identity, "--mount", "type=bind,src="+copyLayout.MemoryDir+",dst=/data", "--mount", "type=bind,src="+stagedBinary+",dst=/ivoai-upgrade-probe,readonly", "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m", "--env-file", credentials, "--entrypoint", "/usr/local/bin/ai-memory", image, "--data-dir", "/data", "serve", "--transport", "http", "--bind", "127.0.0.1:49374", "--no-watcher")
		if err != nil {
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		for {
			if err := runUpgradeDocker(waitCtx, "exec", name, "/ivoai-upgrade-probe", "server", "internal-memory-upgrade-probe"); err == nil {
				if err := runUpgradeDocker(waitCtx, "stop", "--time", "5", name); err != nil {
					return errors.New("candidate could not stop normally after validation")
				}
				afterWiki, err := memoryWikiDigest(ctx, copyLayout.MemoryDir)
				if err != nil || afterWiki != beforeWiki {
					return errors.New("candidate changed authoritative wiki content during copy validation")
				}
				return nil
			}
			if running, err := upgradeContainerRunning(waitCtx, name); err != nil || !running {
				return errors.New("validation runtime exited or became unavailable before the authenticated store probe")
			}
			select {
			case <-waitCtx.Done():
				return errors.New("new ai-memory image did not pass authenticated MCP/unauthenticated refusal on the isolated restored copy")
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
}

// Fixed loopback probe executed by the trusted Core inside the network-isolated
// validation container. It accepts neither an endpoint nor credentials via argv.
func internalMemoryUpgradeProbe(ctx context.Context) error {
	token := os.Getenv("AI_MEMORY_AUTH_TOKEN")
	if len(token) < 32 {
		return errors.New("copy probe requires its transient credential")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	call := func(id int, method string, params any, authorized bool) (int, json.RawMessage, error) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		request, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:49374/mcp", bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if authorized {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := client.Do(request)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		if err != nil {
			return 0, nil, err
		}
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if response.StatusCode == 401 {
			return 401, nil, nil
		}
		if response.StatusCode != 200 || json.Unmarshal(raw, &envelope) != nil || len(envelope.Error) > 0 || len(envelope.Result) == 0 {
			return response.StatusCode, nil, errors.New("copy MCP response invalid")
		}
		return response.StatusCode, envelope.Result, nil
	}
	if status, _, err := call(1, "tools/list", map[string]any{}, false); err != nil || status != 401 {
		return errors.New("copy MCP did not reject unauthenticated access")
	}
	_, listed, err := call(2, "tools/list", map[string]any{}, true)
	if err != nil {
		return errors.New("copy MCP catalog unavailable")
	}
	var tools struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if json.Unmarshal(listed, &tools) != nil {
		return errors.New("copy catalog invalid")
	}
	required := map[string]bool{"memory_status": false, "memory_query": false, "memory_read_page": false}
	for _, tool := range tools.Tools {
		if _, exists := required[tool.Name]; exists {
			required[tool.Name] = true
		}
	}
	for _, present := range required {
		if !present {
			return errors.New("copy MCP contract missing required tool")
		}
	}
	_, result, err := call(3, "tools/call", map[string]any{"name": "memory_status", "arguments": map[string]any{}}, true)
	if err != nil {
		return errors.New("copy database status unavailable")
	}
	var toolResult struct {
		IsError bool              `json:"isError"`
		Content []json.RawMessage `json:"content"`
	}
	if json.Unmarshal(result, &toolResult) != nil || toolResult.IsError || len(toolResult.Content) == 0 {
		return errors.New("copy database status failed")
	}
	return nil
}

func newDockerQdrantValidator(probeBinary, identity, image string) server.DependencyValidator {
	return func(ctx context.Context, copyLayout server.Layout) (validationErr error) {
		parts := strings.Split(identity, ":")
		if len(parts) != 2 {
			return errors.New("invalid qdrant validation identity")
		}
		uid, uerr := strconv.Atoi(parts[0])
		gid, gerr := strconv.Atoi(parts[1])
		if uerr != nil || gerr != nil || uid <= 0 || gid < 0 {
			return errors.New("qdrant validation requires a non-root identity")
		}
		for _, path := range []string{copyLayout.QdrantDir, copyLayout.QdrantInitDir, copyLayout.QdrantSnapshotsDir} {
			if err := secureChownTree(path, uid, gid); err != nil {
				return err
			}
		}
		stagedBinary := filepath.Join(copyLayout.InstallDir, "ivoai-upgrade-probe")
		if _, err := os.Stat(stagedBinary); err != nil {
			return errors.New("trusted staged probe unavailable")
		}
		var entropy [32]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return err
		}
		token := hex.EncodeToString(entropy[:])
		name := "ivoai-qdrant-validation-" + hex.EncodeToString(entropy[:8])
		credentials := filepath.Join(copyLayout.SecretsDir, "qdrant-validation.env")
		if err := platform.AtomicWritePrivate([]byte("QDRANT__SERVICE__API_KEY="+token+"\nQDRANT_INIT_FILE_PATH=/qdrant/init/.qdrant-initialized\n"), credentials); err != nil {
			return err
		}
		defer os.Remove(credentials)
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := runUpgradeDocker(cleanup, "rm", "--force", name); err != nil && validationErr == nil {
				validationErr = errors.New("candidate cleanup failed; runtime assets must not be installed")
			}
		}()
		args := []string{"run", "--detach", "--name", name, "--network", "none", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--pids-limit", "128", "--ulimit", "nproc=512:512", "--ulimit", "nofile=65536:65536", "--memory", "1g", "--cpus", "1", "--user", identity, "--env-file", credentials, "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m"}
		for _, mount := range []struct{ source, target string }{{copyLayout.QdrantDir, "/qdrant/storage"}, {copyLayout.QdrantInitDir, "/qdrant/init"}, {copyLayout.QdrantSnapshotsDir, "/qdrant/snapshots"}, {stagedBinary, "/ivoai-upgrade-probe"}} {
			if strings.ContainsAny(mount.source, ",\n\r") {
				return errors.New("unsupported qdrant validation mount path")
			}
			spec := "type=bind,src=" + mount.source + ",dst=" + mount.target
			if mount.target == "/ivoai-upgrade-probe" {
				spec += ",readonly"
			}
			args = append(args, "--mount", spec)
		}
		args = append(args, image)
		if err := runUpgradeDocker(ctx, args...); err != nil {
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		for {
			if err := runUpgradeDocker(waitCtx, "exec", name, "/ivoai-upgrade-probe", "server", "internal-qdrant-upgrade-probe"); err == nil {
				if err := runUpgradeDocker(waitCtx, "stop", "--time", "5", name); err != nil {
					return errors.New("Qdrant candidate could not stop after validation")
				}
				return nil
			}
			if running, err := upgradeContainerRunning(waitCtx, name); err != nil || !running {
				return errors.New("validation runtime exited or became unavailable before the authenticated store probe")
			}
			select {
			case <-waitCtx.Done():
				return errors.New("new Qdrant image failed authenticated index-read/unauthenticated refusal on the restored copy")
			case <-time.After(300 * time.Millisecond):
			}
		}
	}
}

func internalQdrantUpgradeProbe(ctx context.Context) error {
	token := os.Getenv("QDRANT__SERVICE__API_KEY")
	if len(token) < 32 {
		return errors.New("qdrant copy probe requires its transient credential")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	request := func(method, path string, body []byte, authorized bool) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:6333"+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if authorized {
			req.Header.Set("api-key", token)
		}
		response, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
		return response.StatusCode, data, err
	}
	if status, _, err := request("GET", "/collections", nil, false); err != nil || status != 401 {
		return errors.New("qdrant did not reject unauthenticated collection access")
	}
	status, body, err := request("GET", "/collections", nil, true)
	if err != nil || status != 200 {
		return errors.New("qdrant authenticated collection listing unavailable")
	}
	var listed struct {
		Result struct {
			Collections []struct {
				Name string `json:"name"`
			} `json:"collections"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &listed) != nil {
		return errors.New("qdrant collection response invalid")
	}
	for _, collection := range listed.Result.Collections {
		path := "/collections/" + url.PathEscape(collection.Name)
		status, body, err = request("GET", path, nil, true)
		if err != nil || status != 200 {
			return errors.New("qdrant stored index metadata unavailable")
		}
		var info struct {
			Result struct {
				Status string  `json:"status"`
				Points *uint64 `json:"points_count"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &info) != nil || info.Result.Points == nil || (info.Result.Status != "green" && info.Result.Status != "yellow") {
			return errors.New("qdrant stored index state invalid")
		}
		if status, _, err := request("POST", path+"/points/scroll", []byte(`{"limit":1,"with_payload":false,"with_vector":false}`), true); err != nil || status != 200 {
			return errors.New("qdrant stored point read failed")
		}
	}
	return nil
}

func memoryWikiDigest(ctx context.Context, root string) (string, error) {
	digest := sha256.New()
	wiki := filepath.Join(root, "wiki")
	err := filepath.WalkDir(wiki, func(path string, d os.DirEntry, walkErr error) error {
		if errors.Is(walkErr, os.ErrNotExist) && path == wiki {
			return nil
		}
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("wiki copy contains an unsupported link")
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		relative, err := filepath.Rel(wiki, path)
		if err != nil {
			return err
		}
		fmt.Fprint(digest, relative, "\x00")
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(digest, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprint(digest, "\x00")
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func requireDockerResourceIsolation(ctx context.Context) error {
	command := exec.CommandContext(ctx, "docker", "info", "--format", `{"driver":{{json .CgroupDriver}},"memory":{{.MemoryLimit}},"cpu":{{.CPUCfsQuota}},"pids":{{.PidsLimit}}}`)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return errors.New("dependency upgrade could not verify Docker resource isolation")
	}
	if !dockerResourceIsolationSupported(output.Bytes()) {
		return errors.New("dependency upgrade requires working Docker cgroup memory/CPU/PID isolation; operator configuration is required before changing stores")
	}
	return nil
}

func dockerResourceIsolationSupported(body []byte) bool {
	var info struct {
		Driver string `json:"driver"`
		Memory bool   `json:"memory"`
		CPU    bool   `json:"cpu"`
		PIDs   bool   `json:"pids"`
	}
	if len(body) > 4096 || json.Unmarshal(body, &info) != nil {
		return false
	}
	return (info.Driver == "systemd" || info.Driver == "cgroupfs") && info.Memory && info.CPU && info.PIDs
}

const previousMemoryImage = "akitaonrails/ai-memory@sha256:15178d3fc5bd67439a5b9780b2cf58af476943abea2487514a4c5d6ec18291ee"
const previousQdrantImage = "qdrant/qdrant@sha256:b7b0444c4c351c970b98e90a6f89c2ee4287c65b44e52b4cb503fa5b2aa927ad"

// Validate the old runtime on the backup copy too. This rejects a partially
// migrated store already incompatible with its installed pre-upgrade image,
// instead of issuing a rollback receipt that could not actually start.
func newDockerUpgradeValidator(binary, identity string) server.DependencyValidator {
	return func(ctx context.Context, stage server.Layout) error {
		old, err := platform.ReadRegularFile(filepath.Join(stage.ConfigDir, ".dependency-baseline-compose"), 1<<20)
		if err != nil {
			return errors.New("backup runtime identity unavailable")
		}
		images := map[string]string{}
		service := ""
		for _, line := range strings.Split(string(old), "\n") {
			if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
				service = strings.TrimSuffix(strings.TrimSpace(line), ":")
			}
			if strings.HasPrefix(strings.TrimSpace(line), "image:") {
				images[service] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "image:"))
			}
		}
		if (images["ai-memory"] != previousMemoryImage && images["ai-memory"] != server.MemoryImageReference()) || (images["qdrant"] != previousQdrantImage && images["qdrant"] != server.QdrantImageReference()) {
			return errors.New("previous runtime pins require separate certification before automatic upgrade")
		}
		for _, ref := range []string{images["ai-memory"], images["qdrant"]} {
			if err := runUpgradeDocker(ctx, "image", "inspect", ref); err != nil {
				return errors.New("verified prior runtime image is not locally available for rollback validation")
			}
		}
		if err := newDockerDependencyValidator(binary, identity, images["ai-memory"])(ctx, stage); err != nil {
			return fmt.Errorf("prior memory runtime cannot recover the backup copy: %w", err)
		}
		if err := newDockerQdrantValidator(binary, identity, images["qdrant"])(ctx, stage); err != nil {
			return fmt.Errorf("prior vector runtime cannot recover the snapshot: %w", err)
		}
		if err := os.Remove(filepath.Join(stage.InstallDir, "ivoai-upgrade-probe")); err != nil {
			return err
		}
		if err := newDockerDependencyValidator(binary, identity, server.MemoryImageReference())(ctx, stage); err != nil {
			return err
		}
		return newDockerQdrantValidator(binary, identity, server.QdrantImageReference())(ctx, stage)
	}
}

func upgradeContainerRunning(ctx context.Context, name string) (bool, error) {
	command := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name)
	var out bytes.Buffer
	command.Stdout = &out
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return false, err
	}
	switch strings.TrimSpace(out.String()) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, errors.New("runtime state unavailable")
	}
}
