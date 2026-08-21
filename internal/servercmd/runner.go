package servercmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	contextsvc "github.com/ivo-lopes/ivoai/internal/context"
	"github.com/ivo-lopes/ivoai/internal/enrollment"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/server"
	"github.com/ivo-lopes/ivoai/internal/terminalui"
	"github.com/ivo-lopes/ivoai/internal/webauth"
	"golang.org/x/sys/unix"
)

// New returns the server command adapter consumed by the top-level CLI.
func New(version string) func(context.Context, []string, io.Reader, io.Writer, io.Writer) error {
	return func(ctx context.Context, args []string, in io.Reader, out, errOut io.Writer) error {
		return (&runner{version: version, in: in, out: out, errOut: errOut}).run(ctx, args)
	}
}

type runner struct {
	version string
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
}

func semanticHealth(value string, color bool) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy", "ready", "active":
		return terminalui.Success(value, color)
	case "degraded", "starting", "disabled":
		return terminalui.Warning(value, color)
	default:
		return terminalui.Failure(value, color)
	}
}

func semanticRecordStatus(value string, color bool) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active", "authorized":
		return terminalui.Success(value, color)
	case "pending", "consumed", "expired":
		return terminalui.Warning(value, color)
	default:
		return terminalui.Failure(value, color)
	}
}

func (r *runner) run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		r.usage()
		return nil
	}
	layout, err := serverLayout()
	if err != nil {
		return err
	}
	manager := server.Manager{Layout: layout, Controller: server.SystemdController{}, Architecture: runtime.GOARCH}
	switch args[0] {
	case "setup":
		return r.setup(ctx, layout, manager)
	case "status":
		return r.status(ctx, manager, false)
	case "doctor":
		return r.status(ctx, manager, true)
	case "start":
		return manager.Start(ctx)
	case "stop":
		return manager.Stop(ctx)
	case "restart":
		return manager.Restart(ctx)
	case "logs":
		return r.logs(ctx, manager, args[1:])
	case "enrollment":
		return r.enrollment(layout, args[1:])
	case "web-access":
		return r.webAccess(layout, args[1:])
	case "connector":
		return r.connector(ctx, layout, manager, args[1:])
	case "context":
		return r.context(ctx, layout, args[1:])
	case "memory":
		return r.memory(ctx, layout, args[1:])
	case "backup":
		return r.backup(ctx, layout, manager, args[1:])
	case "restore":
		return r.restore(ctx, layout, manager, args[1:])
	case "gateway":
		return r.gateway(ctx, layout, args[1:])
	case "remote":
		return r.remote(ctx, args[1:])
	default:
		return fmt.Errorf("unknown server command %q", args[0])
	}
}

func serverLayout() (server.Layout, error) {
	root := ""
	if value := os.Getenv("IVOAI_SERVER_ROOT"); value != "" {
		if os.Getenv("IVOAI_TEST_MODE") != "1" {
			return server.Layout{}, errors.New("IVOAI_SERVER_ROOT is accepted only in test mode")
		}
		if !filepath.IsAbs(value) || filepath.Clean(value) == "/" {
			return server.Layout{}, errors.New("IVOAI_SERVER_ROOT must be a non-root absolute path")
		}
		root = filepath.Clean(value)
	}
	return server.DefaultLayout(root), nil
}

func (r *runner) setup(ctx context.Context, layout server.Layout, manager server.Manager) error {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		manager.Controller = nil
		if err := manager.Setup(ctx); err != nil {
			return err
		}
		if err := server.EnsureBackendSecrets(layout); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(layout.DataDir, "enrollment"), 0o700); err != nil {
			return err
		}
		fmt.Fprintln(r.out, "ivoai server setup complete (test mode; services not started)")
		return nil
	}
	if runtime.GOOS != "linux" {
		return errors.New("server mode currently supports Linux only")
	}
	if os.Geteuid() != 0 {
		return errors.New("server setup requires root; run sudo ivoai setup --mode server")
	}
	if err := validateServerOS(); err != nil {
		return err
	}
	if !supportedServerArchitecture(runtime.GOARCH) {
		return fmt.Errorf("unsupported server architecture %s; supported: amd64, arm64", runtime.GOARCH)
	}
	if err := ensureServiceUser(ctx); err != nil {
		return err
	}
	containerUser, err := serviceUserIdentity()
	if err != nil {
		return err
	}
	manager.ContainerUser = containerUser
	if err := ensureDocker(ctx, r.out, r.errOut); err != nil {
		return err
	}
	if err := manager.Setup(ctx); err != nil {
		return err
	}
	if err := server.EnsureBackendSecrets(layout); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(layout.DataDir, "enrollment"), 0o700); err != nil {
		return err
	}
	if err := ensureServiceOwnership(layout); err != nil {
		return err
	}
	// Restart also reconciles updated Compose assets on an idempotent rerun;
	// systemctl start would leave an already-active oneshot dependency unit stale.
	if err := waitForServerStart(ctx, r.out, 15*time.Second, manager.Restart); err != nil {
		diagnosticCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logs, logErr := manager.Logs(diagnosticCtx, "ivoai-dependencies.service", 80)
		if logErr == nil && strings.TrimSpace(logs) != "" {
			return fmt.Errorf("server files installed but services did not start: %w\nRecent dependency journal:\n%s", err, platform.Redact(logs))
		}
		return fmt.Errorf("server files installed but services did not start: %w", err)
	}
	if err := waitForServicesStable(ctx, 20*time.Second, 500*time.Millisecond, 2*time.Second, manager.Status); err != nil {
		diagnosticCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var journals []string
		for _, service := range []string{"ivoai-context.service", "ivoai-gateway.service"} {
			logs, logErr := manager.Logs(diagnosticCtx, service, 40)
			if logErr == nil && strings.TrimSpace(logs) != "" {
				journals = append(journals, service+":\n"+platform.Redact(logs))
			}
		}
		if len(journals) > 0 {
			return fmt.Errorf("server dependencies started but application services are not stable: %w\nRecent service journals:\n%s", err, strings.Join(journals, "\n"))
		}
		return fmt.Errorf("server dependencies started but application services are not stable: %w", err)
	}
	fmt.Fprintf(r.out, "ivoai server %s setup complete\n", r.version)
	return nil
}

func waitForServicesStable(ctx context.Context, timeout, interval, stableFor time.Duration, status func(context.Context) ([]server.ServiceState, error)) error {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	if stableFor <= 0 {
		stableFor = 2 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var stableSince time.Time
	var last []server.ServiceState
	for {
		states, err := status(waitCtx)
		if err != nil {
			return err
		}
		last = states
		allActive := len(states) == len(server.ManagedServices)
		for _, state := range states {
			allActive = allActive && state.Active
		}
		if allActive {
			if stableSince.IsZero() {
				stableSince = time.Now()
			}
			if time.Since(stableSince) >= stableFor {
				return nil
			}
		} else {
			stableSince = time.Time{}
		}
		select {
		case <-waitCtx.Done():
			inactive := make([]string, 0, len(last))
			for _, state := range last {
				if !state.Active {
					inactive = append(inactive, state.Name+"="+state.Detail)
				}
			}
			return fmt.Errorf("timed out waiting for stable services (%s)", strings.Join(inactive, ", "))
		case <-ticker.C:
		}
	}
}

func waitForServerStart(ctx context.Context, out io.Writer, interval time.Duration, start func(context.Context) error) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	fmt.Fprintln(out, "Starting server dependencies; waiting for container health checks...")
	startedAt := time.Now()
	result := make(chan error, 1)
	go func() {
		result <- start(ctx)
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case err := <-result:
			return err
		case <-ticker.C:
			fmt.Fprintf(out, "Server dependencies are still initializing (elapsed %s). Inspect live state with: docker compose -f /etc/ivoai/compose.yaml ps\n", time.Since(startedAt).Round(time.Second))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func validateServerOS() error {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return fmt.Errorf("read /etc/os-release: %w", err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = strings.Trim(value, "\"")
		}
	}
	id, version := values["ID"], values["VERSION_ID"]
	if !supportedServerOS(id, version) {
		return fmt.Errorf("unsupported server OS %s %s; supported: Ubuntu 22.04+, Ubuntu 24.04+, Debian 12+", id, version)
	}
	return nil
}

func supportedServerOS(id, version string) bool {
	parts := strings.SplitN(version, ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	switch id {
	case "debian":
		return major >= 12
	case "ubuntu":
		minor := 0
		if len(parts) > 1 {
			minor, err = strconv.Atoi(parts[1])
			if err != nil {
				return false
			}
		}
		return major > 22 || (major == 22 && minor >= 4)
	default:
		return false
	}
}

func supportedServerArchitecture(architecture string) bool {
	return architecture == "amd64" || architecture == "arm64"
}

func ensureServiceUser(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "id", "-u", "ivoai").Run(); err != nil {
		cmd := exec.CommandContext(ctx, "useradd", "--system", "--home-dir", "/var/lib/ivoai", "--shell", "/usr/sbin/nologin", "--user-group", "ivoai")
		if output, createErr := cmd.CombinedOutput(); createErr != nil {
			return fmt.Errorf("create ivoai container service user: %w: %s", createErr, strings.TrimSpace(string(output)))
		}
	}
	for _, name := range []string{"ivoai-gateway", "ivoai-context"} {
		if err := exec.CommandContext(ctx, "id", "-u", name).Run(); err == nil {
			continue
		}
		cmd := exec.CommandContext(ctx, "useradd", "--system", "--home-dir", "/var/lib/ivoai", "--no-create-home", "--shell", "/usr/sbin/nologin", "--gid", "ivoai", name)
		if output, createErr := cmd.CombinedOutput(); createErr != nil {
			return fmt.Errorf("create %s service user: %w: %s", name, createErr, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func serviceUserIdentity() (string, error) {
	account, err := user.Lookup("ivoai")
	if err != nil {
		return "", fmt.Errorf("lookup ivoai service user: %w", err)
	}
	uid, uidErr := strconv.ParseUint(account.Uid, 10, 32)
	if uidErr != nil || uid == 0 {
		return "", errors.New("ivoai service user must have a non-root numeric UID")
	}
	if _, gidErr := strconv.ParseUint(account.Gid, 10, 32); gidErr != nil {
		return "", errors.New("ivoai service user must have a numeric GID")
	}
	return account.Uid + ":" + account.Gid, nil
}

func ensureServiceOwnership(layout server.Layout) error {
	if os.Geteuid() != 0 {
		return nil
	}
	containerUID, sharedGID, err := serviceAccountIDs("ivoai")
	if err != nil {
		return err
	}
	gatewayUID, _, err := serviceAccountIDs("ivoai-gateway")
	if err != nil {
		return err
	}
	contextUID, _, err := serviceAccountIDs("ivoai-context")
	if err != nil {
		return err
	}

	if err := chownMode(layout.DataDir, 0, sharedGID, 0o750); err != nil {
		return err
	}
	for _, owned := range []struct {
		path string
		uid  int
		mode os.FileMode
	}{
		{layout.ContextDir, contextUID, 0o2750},
		{layout.CorpusDir, contextUID, 0o2750},
		{filepath.Join(layout.DataDir, "enrollment"), gatewayUID, 0o700},
		{layout.MemoryDir, containerUID, 0o700},
		{layout.QdrantDir, containerUID, 0o700},
		{layout.QdrantSnapshotsDir, containerUID, 0o700},
		{layout.QdrantInitDir, containerUID, 0o700},
		{layout.ModelsDir, containerUID, 0o700},
	} {
		// Container and model caches legitimately contain application-managed
		// symlinks (for example Hugging Face snapshot links). Ownership is set on
		// the managed mount root before first start; recursively traversing data
		// created by an unprivileged service is unnecessary and breaks reruns.
		if err := chownMode(owned.path, owned.uid, sharedGID, owned.mode); err != nil {
			return err
		}
	}
	// Enrollment administration runs as root while the gateway runs as its
	// dedicated account. Atomic root writes replace both files with root-owned
	// inodes, so restore their service ownership after every admin operation and
	// idempotent setup. Without this, valid newly-created codes appear invalid.
	enrollmentDir := filepath.Join(layout.DataDir, "enrollment")
	for _, name := range []string{"state.json", "state.json.lock"} {
		if err := secureRegularOwnership(filepath.Join(enrollmentDir, name), gatewayUID, sharedGID, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := chownMode(layout.BackupDir, 0, 0, 0o700); err != nil {
		return err
	}
	if err := chownMode(layout.RuntimeDir, 0, sharedGID, 0o2770); err != nil {
		return err
	}
	if err := chownMode(layout.SecretsDir, 0, sharedGID, 0o710); err != nil {
		return err
	}
	for _, name := range []string{"qdrant.env", "embeddings.env", "memory.env"} {
		if err := secureRegularOwnership(filepath.Join(layout.SecretsDir, name), 0, 0, 0o600); err != nil {
			return err
		}
	}
	tlsDir := filepath.Join(layout.SecretsDir, "tls")
	if _, err := os.Lstat(tlsDir); err == nil {
		if err := secureChownTree(tlsDir, gatewayUID, sharedGID); err != nil {
			return err
		}
		if err := os.Chmod(tlsDir, 0o700); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := secureRegularOwnership(filepath.Join(layout.ContextDir, "catalog.json"), contextUID, sharedGID, 0o640); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Configuration consumed by root-owned systemd/Docker remains root-owned.
	// Services receive group-read access but cannot replace compose or units.
	if err := os.Chown(layout.ConfigDir, 0, sharedGID); err != nil {
		return err
	}
	if err := os.Chmod(layout.ConfigDir, 0o750); err != nil {
		return err
	}
	for _, name := range []string{"server.toml", "gateway.json", "connectors.json", "compose.yaml", "compose.arm64.yaml"} {
		path := filepath.Join(layout.ConfigDir, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe managed server configuration %s", path)
		}
		if err := os.Lchown(path, 0, sharedGID); err != nil {
			return err
		}
		if err := os.Chmod(path, 0o640); err != nil {
			return err
		}
	}
	if err := os.Chown(layout.InstallDir, 0, 0); err != nil {
		return err
	}
	return os.Chmod(layout.InstallDir, 0o755)
}

func serviceAccountIDs(name string) (int, int, error) {
	account, err := user.Lookup(name)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup %s service user: %w", name, err)
	}
	uid, uidErr := strconv.Atoi(account.Uid)
	gid, gidErr := strconv.Atoi(account.Gid)
	if uidErr != nil || gidErr != nil || uid == 0 || gid == 0 {
		return 0, 0, fmt.Errorf("%s service identity must be non-root numeric UID:GID", name)
	}
	return uid, gid, nil
}

func chownMode(path string, uid, gid int, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("unsafe managed directory %s", path)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func secureRegularOwnership(path string, uid, gid int, mode os.FileMode) error {
	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || strings.ContainsRune(name, filepath.Separator) {
		return fmt.Errorf("unsafe managed file %s", path)
	}
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("unsafe managed file %s", path)
	}
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return err
	}
	return unix.Fchmod(fd, uint32(mode.Perm()))
}

// secureChownTree traverses only descriptors opened beneath an already-open
// directory. No path component can be exchanged for a symlink between an
// inspection and a privileged ownership change.
func secureChownTree(root string, uid, gid int) error {
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return secureChownDir(fd, uid, gid)
}

func secureChownDir(fd, uid, gid int) error {
	if err := unix.Fchown(fd, uid, gid); err != nil {
		return err
	}
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return err
	}
	directory := os.NewFile(uintptr(duplicate), "service-owned-directory")
	if directory == nil {
		_ = unix.Close(duplicate)
		return errors.New("open service-owned directory")
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') {
			return errors.New("unsafe service-owned path entry")
		}
		child, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return fmt.Errorf("open service-owned entry %s: %w", name, err)
		}
		var stat unix.Stat_t
		if err := unix.Fstat(child, &stat); err != nil {
			_ = unix.Close(child)
			return err
		}
		mode := stat.Mode & unix.S_IFMT
		switch mode {
		case unix.S_IFDIR:
			err = secureChownDir(child, uid, gid)
		case unix.S_IFREG:
			err = unix.Fchown(child, uid, gid)
		default:
			err = fmt.Errorf("unsupported service-owned entry type %s", name)
		}
		_ = unix.Close(child)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) status(ctx context.Context, manager server.Manager, doctor bool) error {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		if err := manager.Layout.Validate(); err != nil {
			return err
		}
		fmt.Fprintf(r.out, "ivoai server: configured\nlayout: %s\nservices: test-mode\n", manager.Layout.DataDir)
		return nil
	}
	states, err := manager.Status(ctx)
	if err != nil {
		return err
	}
	all := true
	color := terminalui.ColorEnabled(r.out)
	for _, state := range states {
		active := terminalui.Failure("false", color)
		if state.Active {
			active = terminalui.Success("true", color)
		}
		fmt.Fprintf(r.out, "%s\t%s\t%s\n", state.Name, active, platform.Redact(state.Detail))
		all = all && state.Active
	}
	if doctor {
		memoryToken := ""
		for _, secret := range []struct{ file, variable string }{{"qdrant.env", "QDRANT__SERVICE__API_KEY"}, {"embeddings.env", "API_KEY"}, {"memory.env", "AI_MEMORY_AUTH_TOKEN"}} {
			value, err := server.LoadBackendSecret(manager.Layout, secret.file, secret.variable)
			if err != nil {
				return fmt.Errorf("private backend credential %s: %w", secret.file, err)
			}
			if secret.variable == "AI_MEMORY_AUTH_TOKEN" {
				memoryToken = value
			}
		}
		gatewayConfig, configErr := server.LoadGatewayConfig(manager.Layout)
		if configErr != nil {
			return fmt.Errorf("gateway configuration: %w", configErr)
		}
		gatewayBase := "http://127.0.0.1:7744"
		tlsMode := "loopback"
		if gatewayConfig.PublicURL != "" {
			gatewayBase = strings.TrimRight(gatewayConfig.PublicURL, "/")
			tlsMode = "reverse-proxy"
		}
		if gatewayConfig.TLSCertFile != "" {
			tlsMode = "direct"
		}
		gatewayHealth := probeURL(ctx, gatewayBase+"/health")
		contextHealth := probeURL(ctx, gatewayBase+"/ready")
		memoryHealth := probeMemoryMCP(ctx, "http://127.0.0.1:49374/mcp", memoryToken)
		fmt.Fprintf(r.out, "gateway=%s context=%s memory=%s tls=%s databases-public=%s arbitrary-command-api=%s\n", semanticHealth(gatewayHealth, color), semanticHealth(contextHealth, color), semanticHealth(memoryHealth, color), tlsMode, terminalui.Success("false", color), terminalui.Success("false", color))
	}
	if !all {
		return errors.New("one or more ivoai services are inactive")
	}
	return nil
}

func (r *runner) logs(ctx context.Context, manager server.Manager, args []string) error {
	service := "ivoai-gateway.service"
	if len(args) > 0 {
		service = args[0]
	}
	logs, err := manager.Logs(ctx, service, 200)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(r.out, platform.Redact(logs))
	return err
}

func enrollmentStore(layout server.Layout) *enrollment.Store {
	return enrollment.NewStore(filepath.Join(layout.DataDir, "enrollment", "state.json"))
}

func webAccessStore(layout server.Layout) *webauth.Store {
	return webauth.NewStore(filepath.Join(layout.DataDir, "web-oauth", "state.json"))
}

func (r *runner) webAccess(layout server.Layout, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ivoai server web-access <create|list|revoke>")
	}
	store := webAccessStore(layout)
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("server web-access create", flag.ContinueOnError)
		fs.SetOutput(r.errOut)
		ttl := fs.Duration("ttl", 10*time.Minute, "activation lifetime")
		scopeText := fs.String("scopes", strings.Join(webauth.DefaultScopes, ","), "comma-separated OAuth scopes")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		scopes := strings.Split(*scopeText, ",")
		created, err := store.CreateActivation(*ttl, scopes)
		if err != nil {
			return err
		}
		if err := ensureServiceOwnership(layout); err != nil {
			return err
		}
		fmt.Fprintf(r.out, "Web access ID: %s\nExpires: %s\nActivation code (shown once): %s\n", created.ID, created.ExpiresAt.Format(time.RFC3339), created.Code)
		return nil
	case "list":
		items, err := store.ListGrants()
		if err != nil {
			return err
		}
		for _, item := range items {
			fmt.Fprintf(r.out, "%s\t%s\t%s\t%s\n", item.ID, semanticRecordStatus(item.Status, terminalui.ColorEnabled(r.out)), item.ExpiresAt.Format(time.RFC3339), strings.Join(item.Scopes, ","))
		}
		return nil
	case "revoke":
		if len(args) != 2 {
			return errors.New("usage: ivoai server web-access revoke <id>")
		}
		if err := store.RevokeActivation(args[1]); err != nil {
			return err
		}
		return ensureServiceOwnership(layout)
	default:
		return fmt.Errorf("unknown web-access action %q", args[0])
	}
}

func (r *runner) enrollment(layout server.Layout, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: ivoai server enrollment <create|list|revoke>")
	}
	store := enrollmentStore(layout)
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("server enrollment create", flag.ContinueOnError)
		fs.SetOutput(r.errOut)
		ttl := fs.Duration("ttl", 10*time.Minute, "one-time code lifetime")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		created, err := store.Create(*ttl, nil)
		if err != nil {
			return err
		}
		if err := ensureServiceOwnership(layout); err != nil {
			return err
		}
		fmt.Fprintf(r.out, "Enrollment ID: %s\nExpires: %s\nEnrollment code (shown once): %s\n", created.ID, created.ExpiresAt.Format(time.RFC3339), created.Code)
		return nil
	case "list":
		items, err := store.List()
		if err != nil {
			return err
		}
		for _, item := range items {
			status := "active"
			if !item.RevokedAt.IsZero() {
				status = "revoked"
			} else if !item.ConsumedAt.IsZero() {
				status = "consumed"
			} else if time.Now().After(item.ExpiresAt) {
				status = "expired"
			}
			fmt.Fprintf(r.out, "%s\t%s\t%s\n", item.ID, semanticRecordStatus(status, terminalui.ColorEnabled(r.out)), item.ExpiresAt.Format(time.RFC3339))
		}
		return nil
	case "revoke":
		if len(args) != 2 {
			return errors.New("usage: ivoai server enrollment revoke <id>")
		}
		if err := store.Revoke(args[1]); err != nil {
			return err
		}
		return ensureServiceOwnership(layout)
	default:
		return fmt.Errorf("unknown enrollment action %q", args[0])
	}
}

type connectorRecord struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

func connectorPath(layout server.Layout) string {
	return filepath.Join(layout.ConfigDir, "connectors.json")
}

func loadConnectors(layout server.Layout) ([]connectorRecord, error) {
	b, err := os.ReadFile(connectorPath(layout))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []connectorRecord
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, fmt.Errorf("parse connector registry: %w", err)
	}
	return records, nil
}

func saveConnectors(layout server.Layout, records []connectorRecord) error {
	sort.Slice(records, func(i, j int) bool { return records[i].Name < records[j].Name })
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return platform.AtomicWritePrivate(append(b, '\n'), connectorPath(layout))
}

func (r *runner) connector(ctx context.Context, layout server.Layout, manager server.Manager, args []string) error {
	if len(args) == 0 || args[0] == "list" {
		records, err := loadConnectors(layout)
		if err != nil {
			return err
		}
		for _, record := range records {
			fmt.Fprintf(r.out, "%s\t%s\t%s\n", record.Name, record.Kind, record.Path)
		}
		return nil
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("server connector add", flag.ContinueOnError)
		fs.SetOutput(r.errOut)
		kind := fs.String("type", "filesystem", "filesystem or git")
		name := fs.String("name", "", "unique connector name")
		path := fs.String("path", "", "absolute source path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || (*kind != "filesystem" && *kind != "git") || !filepath.IsAbs(*path) {
			return errors.New("connector add requires --name, --type filesystem|git, and an absolute --path")
		}
		if _, err := os.Stat(*path); err != nil {
			return fmt.Errorf("connector source: %w", err)
		}
		records, err := loadConnectors(layout)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.Name == *name {
				return fmt.Errorf("connector %q already exists", *name)
			}
		}
		records = append(records, connectorRecord{Name: *name, Kind: *kind, Path: filepath.Clean(*path)})
		if err := saveConnectors(layout, records); err != nil {
			return err
		}
		if err := ensureServiceOwnership(layout); err != nil {
			return err
		}
		if os.Getenv("IVOAI_TEST_MODE") != "1" {
			_ = exec.CommandContext(ctx, "systemctl", "restart", "ivoai-context.service", "ivoai-gateway.service").Run()
		}
		return nil
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: ivoai server connector remove <name>")
		}
		records, err := loadConnectors(layout)
		if err != nil {
			return err
		}
		filtered := records[:0]
		for _, record := range records {
			if record.Name != args[1] {
				filtered = append(filtered, record)
			}
		}
		if len(filtered) == len(records) {
			return fmt.Errorf("connector %q not found", args[1])
		}
		return r.withQuiescedContext(ctx, manager, func() error {
			service, err := contextService(layout)
			if os.Getenv("IVOAI_TEST_MODE") == "1" {
				service, err = contextsvc.NewService(contextsvc.DeterministicEmbedder{DimensionsN: 384}, contextsvc.NewMemoryStore(), &contextsvc.FileCatalog{Path: filepath.Join(layout.ContextDir, "catalog.json")})
			}
			if err != nil {
				return err
			}
			if err := service.Initialize(ctx); err != nil {
				return fmt.Errorf("initialize context purge: %w", err)
			}
			if err := service.PurgeSource(ctx, args[1]); err != nil {
				return fmt.Errorf("purge connector %q context: %w", args[1], err)
			}
			if err := saveConnectors(layout, filtered); err != nil {
				return err
			}
			return ensureServiceOwnership(layout)
		})
	default:
		return fmt.Errorf("unknown connector action %q", args[0])
	}
}

func (r *runner) withQuiescedContext(ctx context.Context, manager server.Manager, operation func() error) error {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return operation()
	}
	services := []string{"ivoai-gateway.service", "ivoai-context.service"}
	if manager.Controller == nil {
		return errors.New("service controller unavailable for atomic connector removal")
	}
	if err := manager.Controller.Stop(ctx, services); err != nil {
		return fmt.Errorf("quiesce context services: %w", err)
	}
	operationErr := operation()
	restartErr := manager.Controller.Start(ctx, []string{"ivoai-context.service", "ivoai-gateway.service"})
	if operationErr != nil && restartErr != nil {
		return errors.Join(operationErr, fmt.Errorf("restart context services: %w", restartErr))
	}
	if operationErr != nil {
		return operationErr
	}
	if restartErr != nil {
		return fmt.Errorf("connector removed but context services did not restart: %w", restartErr)
	}
	return nil
}

func addConfiguredConnectors(service *contextsvc.Service, layout server.Layout) error {
	records, err := loadConnectors(layout)
	if err != nil {
		return err
	}
	for _, record := range records {
		var connector contextsvc.Connector
		if record.Kind == "git" {
			connector = contextsvc.GitConnector{ConnectorName: record.Name, Repository: record.Path}
		} else {
			connector = contextsvc.FilesystemConnector{ConnectorName: record.Name, Root: record.Path}
		}
		if err := service.AddConnector(connector); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) context(ctx context.Context, layout server.Layout, args []string) error {
	if len(args) == 1 && args[0] == "serve" {
		return serveContext(ctx, layout, r.errOut)
	}
	if len(args) == 0 || args[0] == "status" {
		service, err := contextService(layout)
		if err != nil {
			return err
		}
		if err := addConfiguredConnectors(service, layout); err != nil {
			return err
		}
		status := service.Status(ctx)
		fmt.Fprintf(r.out, "Context service: %s\nDocuments: %d\nChunks: %d\nConnectors: %d\n", semanticHealth(map[bool]string{true: "healthy", false: "unhealthy"}[status.Healthy], terminalui.ColorEnabled(r.out)), status.Documents, status.Chunks, status.Connectors)
		return nil
	}
	return errors.New("usage: ivoai server context [status|serve]")
}

func (r *runner) memory(ctx context.Context, layout server.Layout, args []string) error {
	if len(args) > 1 || (len(args) == 1 && args[0] != "status") {
		return errors.New("usage: ivoai server memory status")
	}
	token, err := server.LoadBackendSecret(layout, "memory.env", "AI_MEMORY_AUTH_TOKEN")
	if err != nil {
		return fmt.Errorf("private backend credential memory.env: %w", err)
	}
	status := probeMemoryMCP(ctx, "http://127.0.0.1:49374/mcp", token)
	fmt.Fprintf(r.out, "ai-memory: %s\n", semanticHealth(status, terminalui.ColorEnabled(r.out)))
	if status != "healthy" {
		return errors.New("ai-memory is unavailable; context and agent clients remain usable")
	}
	return nil
}

func (r *runner) backup(ctx context.Context, layout server.Layout, manager server.Manager, args []string) error {
	fs := flag.NewFlagSet("server backup", flag.ContinueOnError)
	fs.SetOutput(r.errOut)
	output := fs.String("output", "", "absolute backup archive path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		*output = filepath.Join(layout.BackupDir, "ivoai-"+time.Now().UTC().Format("20060102T150405Z")+".tar.gz")
	}
	if err := r.withQuiescedServices(ctx, manager, func() error { return server.Backup(layout, *output, time.Now()) }); err != nil {
		return err
	}
	fmt.Fprintln(r.out, *output)
	return nil
}

func (r *runner) restore(ctx context.Context, layout server.Layout, manager server.Manager, args []string) error {
	fs := flag.NewFlagSet("server restore", flag.ContinueOnError)
	fs.SetOutput(r.errOut)
	input := fs.String("input", "", "absolute backup archive path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *input == "" && fs.NArg() == 1 {
		*input = fs.Arg(0)
	}
	if *input == "" {
		return errors.New("usage: ivoai server restore --input <absolute-backup-path>")
	}
	return r.withQuiescedServices(ctx, manager, func() error {
		if err := server.Restore(layout, *input); err != nil {
			return err
		}
		return ensureServiceOwnership(layout)
	})
}

func (r *runner) withQuiescedServices(ctx context.Context, manager server.Manager, operation func() error) error {
	if os.Getenv("IVOAI_TEST_MODE") == "1" {
		return operation()
	}
	if err := manager.Stop(ctx); err != nil {
		return fmt.Errorf("quiesce services: %w", err)
	}
	operationErr := operation()
	restartErr := manager.Start(ctx)
	if operationErr != nil && restartErr != nil {
		return errors.Join(operationErr, fmt.Errorf("restart services after operation: %w", restartErr))
	}
	if operationErr != nil {
		return operationErr
	}
	if restartErr != nil {
		return fmt.Errorf("operation completed but services did not restart: %w", restartErr)
	}
	return nil
}

func (r *runner) gateway(ctx context.Context, layout server.Layout, args []string) error {
	if len(args) == 1 && args[0] == "serve" {
		return serveGateway(ctx, layout, r.version, r.errOut)
	}
	if len(args) != 0 && args[0] == "configure" {
		fs := flag.NewFlagSet("server gateway configure", flag.ContinueOnError)
		fs.SetOutput(r.errOut)
		publicURL := fs.String("public-url", "", "public HTTPS origin")
		listen := fs.String("listen", "127.0.0.1:7744", "gateway listen address")
		cert := fs.String("tls-cert", "", "absolute TLS certificate path")
		key := fs.String("tls-key", "", "absolute owner-only TLS private key path")
		var trustedProxies stringListFlag
		fs.Var(&trustedProxies, "trusted-proxy", "trusted reverse-proxy source CIDR (repeatable)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *publicURL == "" {
			return errors.New("gateway configure requires --public-url https://host")
		}
		managedCert, managedKey := *cert, *key
		if *cert != "" && *key != "" {
			var err error
			managedCert, managedKey, err = server.InstallGatewayTLS(layout, *cert, *key)
			if err != nil {
				return err
			}
		}
		config := server.GatewayConfig{ListenAddress: *listen, PublicURL: *publicURL, TLSCertFile: managedCert, TLSKeyFile: managedKey, TrustedProxyCIDRs: trustedProxies}
		if err := server.SaveGatewayConfig(layout, config); err != nil {
			return err
		}
		if err := ensureServiceOwnership(layout); err != nil {
			return err
		}
		if os.Getenv("IVOAI_TEST_MODE") != "1" {
			if output, err := exec.CommandContext(ctx, "systemctl", "restart", "ivoai-gateway.service").CombinedOutput(); err != nil {
				return fmt.Errorf("gateway configured but restart failed: %w: %s", err, platform.Redact(strings.TrimSpace(string(output))))
			}
		}
		mode := "TLS reverse proxy on loopback"
		if *cert != "" {
			mode = "direct TLS"
		} else if len(trustedProxies) > 0 {
			mode = "trusted HTTPS reverse proxy"
		}
		fmt.Fprintf(r.out, "Gateway configured: %s (%s)\n", *publicURL, mode)
		return nil
	}
	return errors.New("usage: ivoai server gateway <serve|configure --public-url HTTPS_ORIGIN [--listen HOST:PORT] [--trusted-proxy CIDR] [--tls-cert PATH --tls-key PATH]>")
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value must not be empty")
	}
	*f = append(*f, value)
	return nil
}

func (r *runner) usage() {
	fmt.Fprintln(r.out, `ivoai server commands:
  setup | status | doctor | start | stop | restart | logs [service]
  enrollment create [--ttl 10m] | list | revoke <id>
  web-access create [--ttl 10m] [--scopes SCOPE,...] | list | revoke <id>
  connector list | add --name NAME --type filesystem|git --path PATH | remove NAME
  context status | memory status
  gateway configure --public-url HTTPS_ORIGIN [--listen HOST:PORT] [--trusted-proxy CIDR] [--tls-cert PATH --tls-key PATH]
  backup [--output PATH] | restore --input PATH
  remote status | doctor | connector list`)
}
