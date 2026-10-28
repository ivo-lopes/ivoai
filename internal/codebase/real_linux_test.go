//go:build linux

package codebase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/unix"
)

// This test reuses an explicitly prepared synthetic index. It cannot index: the
// actual server uses analysis and the SPI only permits the two reader tools.
func TestRealPinnedCodebasePreviouslyIndexedFixture(t *testing.T) {
	binary, fixture := os.Getenv("IVOAI_TEST_CODEBASE_BINARY"), os.Getenv("IVOAI_TEST_CODEBASE_FIXTURE")
	if binary == "" || fixture == "" || runtime.GOARCH != "amd64" {
		t.Skip("NOT_RUN: explicit pinned Linux amd64 binary and pre-indexed synthetic fixture required")
	}
	if !filepath.IsAbs(binary) || !filepath.IsAbs(fixture) {
		t.Fatal("explicit absolute fixture paths required")
	}
	hashFile := func(path string) string {
		t.Helper()
		f := openFixtureRegular(t, path, 400<<20)
		defer f.Close()
		h := sha256.New()
		if _, e := io.Copy(h, f); e != nil {
			t.Fatal("fixture hash unavailable")
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	if hashFile(binary) != BinarySHA256 {
		t.Fatal("binary pin mismatch")
	}
	runtimeInfo, err := os.Lstat(binary)
	if err != nil || runtimeInfo.Mode().Perm()&0022 != 0 {
		t.Fatal("runtime identity unavailable or writable by group/others")
	}
	runtimeStat, ok := runtimeInfo.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("runtime ctime identity unavailable")
	}
	runtimeCTime := runtimeStat.Ctim
	root := t.TempDir()
	source, cache := filepath.Join(root, "source"), filepath.Join(root, "cache")
	for _, name := range []string{"source", "cache", "runtime", "home", "tmp"} {
		if os.Mkdir(filepath.Join(root, name), 0700) != nil {
			t.Fatal("private fixture creation failed")
		}
	}
	expected := map[string]string{"go.mod": "b55ac3345be8d45563b07bbfdf3f815f6d645f910ecdf24760c0af4755c6b74d", "entry.go": "bd368dab089c5e8bc93eb4fe1df2f3c69606a1cb837366a250104effaba4f5c0", "helper.go": "52e6cb8715a079f2e52fa81f69460d6b37157621291e5041742c255adf7b32cc", "unused.go": "96d41f38bfd3ef5ecb50424f0efe0969c35eed744ce28e0b0b8a81689359829c", "entry_test.go": "d46247b8e7ae43c9f5d5e656d364bf4033e91403ea5d893df8bd8512c24897fe"}
	for name, digest := range expected {
		old := filepath.Join(fixture, "source", name)
		if hashFile(old) != digest {
			t.Fatal("synthetic source identity mismatch")
		}
		file := openFixtureRegular(t, old, 4096)
		raw, e := io.ReadAll(file)
		file.Close()
		if e != nil || os.WriteFile(filepath.Join(source, name), raw, 0400) != nil {
			t.Fatal("synthetic source copy failed")
		}
	}
	const database = "ivoai_synthetic.db"
	original := filepath.Join(fixture, "cache", database)
	originalDigest := hashFile(original)
	db := openFixtureRegular(t, original, 16<<20)
	copied, e := os.OpenFile(filepath.Join(cache, database), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("private index copy unavailable")
	}
	n, e := io.Copy(copied, io.LimitReader(db, 16<<20))
	db.Close()
	copied.Close()
	if e != nil || n == 16<<20 || hashFile(filepath.Join(cache, database)) != originalDigest {
		t.Fatal("prior index copy mismatch")
	}
	// Python is only a trusted capsule launcher/settings seeder, not an MCP
	// implementation. Protocol framing/discovery/cancellation use the existing SDK.
	const capsule = `import os,sys,resource,sqlite3,json
binary,base=sys.argv[1:]
cache=base+'/cache'
c=sqlite3.connect(cache+'/_config.db');c.execute('CREATE TABLE config (key TEXT PRIMARY KEY,value TEXT)');c.executemany('INSERT INTO config VALUES (?,?)',[(k,'false') for k in ['auto_index','auto_watch','watcher_enabled','doc_candidates']]);c.commit();c.close();os.chmod(cache+'/_config.db',0o600)
with open(cache+'/config.json','w') as f:json.dump({'ui_enabled':False,'ui_port':9749},f)
os.chmod(cache+'/config.json',0o600)
resource.setrlimit(resource.RLIMIT_CPU,(20,20));resource.setrlimit(resource.RLIMIT_AS,(4<<30,4<<30));resource.setrlimit(resource.RLIMIT_NPROC,(2048,2048));resource.setrlimit(resource.RLIMIT_NOFILE,(128,128));resource.setrlimit(resource.RLIMIT_FSIZE,(64<<20,64<<20));resource.setrlimit(resource.RLIMIT_CORE,(0,0));os.umask(0o077)
a=['/usr/bin/bwrap','--unshare-all','--die-with-parent','--new-session','--clearenv','--dir','/view','--dir','/workspace','--dir','/workspace/project','--dev','/dev','--proc','/proc','--ro-bind',binary,'/view/cbm','--ro-bind',base+'/source','/workspace/project']
for name,target in [('cache','/cache'),('runtime','/runtime'),('home','/private'),('tmp','/tmp')]:a+=['--bind',base+'/'+name,target]
e={'HOME':'/private','XDG_CONFIG_HOME':'/private/config','XDG_DATA_HOME':'/private/data','XDG_CACHE_HOME':'/cache','XDG_RUNTIME_DIR':'/runtime','CBM_CACHE_DIR':'/cache','CBM_RUNTIME_DIR':'/runtime','CBM_ALLOWED_ROOT':'/workspace/project','CBM_WORKERS':'1','CBM_MEM_BUDGET_MB':'512','CBM_MAX_FILE_BYTES':'1048576','CBM_INDEX_MAX_RESTARTS':'1','CBM_INDEX_WORKER_TIMEOUT_S':'10','CBM_STARTUP_TIMEOUT_MS':'10000','CBM_SEMANTIC_ENABLED':'false','CBM_DIAGNOSTICS':'false','CBM_PROFILE':'false','CBM_LOG_LEVEL':'none','CBM_MEM_STATS':'false','CBM_MEM_PHASES':'false','CBM_MEM_CENSUS':'false','PATH':'/nonexistent','LC_ALL':'C','TZ':'UTC'}
for k,v in e.items():a+=['--setenv',k,v]
a+=['--chdir','/workspace/project','--remount-ro','/','--','/view/cbm','--tool-profile=analysis','--ui=false']
os.execve(a[0],a,{'PATH':'/nonexistent','LC_ALL':'C'})`
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-c", capsule, binary, root)
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	stdout, e := command.StdoutPipe()
	if e != nil {
		t.Fatal("capsule stdout unavailable")
	}
	stdin, e := command.StdinPipe()
	if e != nil {
		t.Fatal("capsule stdin unavailable")
	}
	if command.Start() != nil {
		t.Fatal("capsule start failed")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "ivoai-codebase-read-fixture", Version: "1"}, nil)
	session, e := client.Connect(ctx, &mcp.IOTransport{Reader: stdout, Writer: stdin, MaxLineLength: MaxReplyBytes}, nil)
	defer func() {
		if session != nil {
			session.Close()
		}
		stdin.Close()
		cancel()
		_ = command.Wait()
	}()
	if e != nil {
		t.Fatal("real pinned MCP initialization unavailable; no pass claimed")
	}
	receipt := fixtureReceipt()
	receipt.Project = "ivoai_synthetic"
	receipt.SourceGeneration = evidenceDigestFiles(t, source, expected)
	receipt.IndexDigest = hashFile(filepath.Join(cache, database))
	callCount := 0
	o := Options{Enabled: true, Receipt: receipt, Authorize: func(ctx context.Context, r Receipt) error {
		if r != receipt {
			return ErrDenied
		}
		return ctx.Err()
	}, CurrentGeneration: func(ctx context.Context) (Receipt, error) {
		if ctx.Err() != nil {
			return Receipt{}, ctx.Err()
		}
		r := receipt
		r.SourceGeneration = evidenceDigestFiles(t, source, expected)
		r.IndexDigest = hashFile(filepath.Join(cache, database))
		currentInfo, statErr := os.Lstat(binary)
		if statErr != nil || !currentInfo.Mode().IsRegular() || !os.SameFile(runtimeInfo, currentInfo) || currentInfo.Size() != runtimeInfo.Size() || currentInfo.Mode() != runtimeInfo.Mode() || !currentInfo.ModTime().Equal(runtimeInfo.ModTime()) {
			return Receipt{}, ErrDenied
		}
		currentStat, ok := currentInfo.Sys().(*syscall.Stat_t)
		if !ok || currentStat.Ctim != runtimeCTime {
			return Receipt{}, ErrDenied
		}
		return r, nil
	}, Call: func(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
		if name != "search_graph" && name != "trace_path" {
			t.Fatal("adapter attempted non-read operation")
		}
		callCount++
		response, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			return nil, err
		}
		return json.Marshal(response)
	}}
	p := mustProvider(t, o)
	ask, err := p.Query(ctx, core.CodeQuery{Operation: "ask", Query: "Helper"})
	if err != nil {
		t.Fatalf("real symbol adapter rejected pinned schema: %v", err)
	}
	var symbols symbolPage
	if json.Unmarshal([]byte(ask.Text), &symbols) != nil || len(symbols.Symbols) != 1 || symbols.Symbols[0].File != "helper.go" || ask.Truncated {
		t.Fatal("real exact-symbol oracle failed")
	}
	callers, err := p.Query(ctx, core.CodeQuery{Operation: "callers", Symbol: "Helper"})
	if err != nil {
		t.Fatalf("real caller adapter rejected pinned schema: %v", err)
	}
	var trace tracePage
	if json.Unmarshal([]byte(callers.Text), &trace) != nil || trace.Partial || callers.Truncated {
		t.Fatal("real trace shape or completeness failed")
	}
	direct := 0
	entry := false
	for _, row := range trace.Rows {
		if row.Hop == 1 {
			direct++
			entry = strings.HasSuffix(row.QualifiedName, ".Entry") || row.QualifiedName == "Entry"
			if strings.Contains(row.QualifiedName, "Unused") {
				t.Fatal("false direct caller")
			}
		}
	}
	if direct != 1 || !entry || callCount != 3 {
		t.Fatal("real direct-caller oracle failed")
	}
	if hashFile(binary) != BinarySHA256 {
		t.Fatal("runtime full-session integrity changed")
	}
	if hashFile(filepath.Join(cache, database)) != originalDigest || hashFile(original) != originalDigest {
		t.Fatal("reader mutated the prior index")
	}
	t.Logf("CODEBASE_REAL_ADAPTER symbols=1 direct_callers=1 calls=%d readonly_tools=true index_unchanged=true binary_sha256=%s", callCount, BinarySHA256)
}
func evidenceDigestFiles(t *testing.T, root string, expected map[string]string) string {
	t.Helper()
	h := sha256.New()
	names := []string{"entry.go", "entry_test.go", "go.mod", "helper.go", "unused.go"}
	for _, name := range names {
		file := openFixtureRegular(t, filepath.Join(root, name), 4096)
		body, e := io.ReadAll(file)
		file.Close()
		if e != nil {
			t.Fatal("fixture source unavailable")
		}
		d := sha256.Sum256(body)
		if hex.EncodeToString(d[:]) != expected[name] {
			t.Fatal("fixture source changed")
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(d[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Fixture descriptors never follow a leaf link or open/block on a FIFO. Parents
// are host-owned private staging paths; malicious host/root/same-UID replacement
// of the whole namespace is outside this proof's threat boundary.
func openFixtureRegular(t *testing.T, path string, max int64) *os.File {
	t.Helper()
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > max {
		t.Fatal("invalid fixture regular file")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal("fixture descriptor unavailable")
	}
	f := os.NewFile(uintptr(fd), path)
	after, err := f.Stat()
	if err != nil || !after.Mode().IsRegular() || after.Size() > max || !os.SameFile(before, after) {
		f.Close()
		t.Fatal("fixture identity changed")
	}
	return f
}
