package benchmarkfixtures

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var ErrEditContract = errors.New("edit contract or executable oracle not satisfied")

// EditSnapshot includes Git metadata as well as source. It is host data, not a
// permission grant. Files are copied once into bounded memory before validation.
type EditSnapshot struct {
	files  map[string][]byte
	modes  map[string]fs.FileMode
	dirs   map[string]fs.FileMode
	Digest string
}

func SnapshotEditTree(root string) (EditSnapshot, error) {
	s := EditSnapshot{files: map[string][]byte{}, modes: map[string]fs.FileMode{}, dirs: map[string]fs.FileMode{}}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			info, err := entry.Info()
			rel, relErr := filepath.Rel(root, path)
			if err != nil || relErr != nil || len(s.dirs) >= 128 {
				return ErrEditContract
			}
			s.dirs[filepath.ToSlash(rel)] = info.Mode().Perm()
			return nil
		}
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err != nil {
			return ErrEditContract
		}
		file := os.NewFile(uintptr(fd), path)
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 || len(s.files) >= 128 {
			file.Close()
			return ErrEditContract
		}
		body := &boundedOracleOutput{limit: 1 << 20}
		_, err = io.Copy(body, io.LimitReader(file, (1<<20)+1))
		file.Close()
		if err != nil || body.overflow {
			return ErrEditContract
		}
		total += int64(body.Len())
		if total > 1<<20 {
			return ErrEditContract
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return ErrEditContract
		}
		s.files[filepath.ToSlash(rel)] = bytes.Clone(body.Bytes())
		s.modes[filepath.ToSlash(rel)] = info.Mode().Perm()
		return nil
	})
	if err != nil {
		return EditSnapshot{}, ErrEditContract
	}
	names := make([]string, 0, len(s.files))
	for name := range s.files {
		names = append(names, name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		hash.Write([]byte(name + "\x00"))
		hash.Write([]byte(s.modes[name].String() + "\x00"))
		hash.Write(s.files[name])
		hash.Write([]byte{0})
	}
	names = names[:0]
	for name := range s.dirs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		hash.Write([]byte("directory\x00" + name + "\x00" + s.dirs[name].String() + "\x00"))
	}
	s.Digest = hex.EncodeToString(hash.Sum(nil))
	return s, nil
}

func EditPaths(id string) []string {
	switch id {
	case "small_known_file_correction":
		return []string{"mathpkg/clamp.go"}
	case "cross_module_refactor":
		return []string{"packages/shared/src/discount.ts", "packages/shop/src/main.ts", "packages/admin/src/report.ts"}
	}
	return nil
}

// ValidateEdit requires every declared file to change and forbids all other
// changes, additions and deletions, including Git metadata and oracle tests.
// Static pure-code/token restrictions run before any generated code executes.
func ValidateEdit(id string, before, after EditSnapshot) bool {
	allowed := EditPaths(id)
	if len(allowed) == 0 || len(before.files) != len(after.files) || len(before.dirs) != len(after.dirs) {
		return false
	}
	for _, name := range allowed {
		if _, present := before.files[name]; !present {
			return false
		}
	}
	for name, mode := range before.dirs {
		if after.dirs[name] != mode {
			return false
		}
	}
	for name, original := range before.files {
		body, ok := after.files[name]
		if !ok || before.modes[name] != after.modes[name] {
			return false
		}
		mutable := false
		for _, path := range allowed {
			mutable = mutable || path == name
		}
		if mutable == bytes.Equal(original, body) {
			return false
		}
		if mutable && id == "cross_module_refactor" && !identifierOnlyRename(original, body) {
			return false
		}
	}
	if id == "small_known_file_correction" {
		return pureClamp(after.files[allowed[0]])
	}
	return true
}

func pureClamp(body []byte) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "clamp.go", body, 0)
	if err != nil || file.Name.Name != "mathpkg" || len(file.Decls) != 1 || len(file.Imports) != 0 {
		return false
	}
	function, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || function.Name.Name != "Clamp" || function.Recv != nil || function.Body == nil || function.Type.TypeParams != nil || function.Type.Results == nil || len(function.Type.Results.List) != 1 || len(function.Type.Results.List[0].Names) != 0 {
		return false
	}
	result, ok := function.Type.Results.List[0].Type.(*ast.Ident)
	if !ok || result.Name != "int" {
		return false
	}
	names := []string{}
	for _, field := range function.Type.Params.List {
		typeName, ok := field.Type.(*ast.Ident)
		if !ok || typeName.Name != "int" {
			return false
		}
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	if strings.Join(names, ",") != "value,min,max" {
		return false
	}
	valid := true
	nodes, depth := 0, 0
	ast.Inspect(file, func(node ast.Node) bool {
		if !valid {
			return false
		}
		if node == nil {
			depth--
			return valid
		}
		nodes++
		depth++
		if nodes > 256 || depth > 32 {
			valid = false
			return false
		}
		switch n := node.(type) {
		case *ast.File, *ast.FuncDecl, *ast.FuncType, *ast.FieldList, *ast.Field, *ast.BlockStmt, *ast.IfStmt, *ast.ParenExpr:
		case *ast.BinaryExpr:
			switch n.Op {
			case token.LSS, token.GTR, token.LEQ, token.GEQ, token.EQL, token.NEQ, token.LAND, token.LOR:
			default:
				valid = false
			}
		case *ast.UnaryExpr:
			if n.Op != token.NOT {
				valid = false
			}
		case *ast.ReturnStmt:
			if len(n.Results) != 1 {
				valid = false
			} else if id, ok := n.Results[0].(*ast.Ident); !ok || id.Name != "value" && id.Name != "min" && id.Name != "max" {
				valid = false
			}
		case *ast.Ident:
			if n.Name != "mathpkg" && n.Name != "Clamp" && n.Name != "int" && n.Name != "value" && n.Name != "min" && n.Name != "max" && n.Name != "true" && n.Name != "false" {
				valid = false
			}
		default:
			valid = false
		}
		return valid
	})
	return valid
}

func identifierOnlyRename(original, edited []byte) bool {
	lex := func(body []byte, rename bool) ([]string, bool) {
		fset := token.NewFileSet()
		var lexer scanner.Scanner
		failed := false
		lexer.Init(fset.AddFile("fixture.ts", -1, len(body)), body, func(token.Position, string) { failed = true }, 0)
		out := []string{}
		for {
			_, kind, literal := lexer.Scan()
			if kind == token.EOF {
				return out, !failed
			}
			if kind == token.SEMICOLON {
				continue
			}
			if rename && kind == token.IDENT && literal == "discount" {
				literal = "applyDiscount"
			}
			out = append(out, strconv.Itoa(int(kind))+":"+literal)
		}
	}
	a, okA := lex(original, true)
	b, okB := lex(edited, false)
	return okA && okB && strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

type OracleTools struct{ Go, Node, Bwrap string }

func OracleIdentity(id string, tools OracleTools) (string, error) {
	tool := tools.Go
	if id == "cross_module_refactor" {
		tool = tools.Node
	} else if id != "small_known_file_correction" {
		return "", ErrEditContract
	}
	hash := sha256.New()
	hash.Write([]byte(id + "\x00"))
	for _, path := range []string{tool, tools.Bwrap, "/usr/bin/prlimit"} {
		info, err := os.Stat(path)
		if !filepath.IsAbs(path) || err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 || info.Size() > 512<<20 {
			return "", ErrEditContract
		}
		file, err := os.Open(path)
		if err != nil {
			return "", ErrEditContract
		}
		digest := sha256.New()
		n, err := io.Copy(digest, io.LimitReader(file, (512<<20)+1))
		file.Close()
		if err != nil || n > 512<<20 {
			return "", ErrEditContract
		}
		hash.Write([]byte(hex.EncodeToString(digest.Sum(nil)) + "\x00"))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ProbeEditOracle starts only the fixed system true command in the exact
// containment used by the oracle. It never runs project code or a provider.
func ProbeEditOracle(ctx context.Context, id, root string, tools OracleTools) error {
	_, err := runEditSandbox(ctx, id, root, tools, []string{"/usr/bin/true"})
	return err
}

// RunEditOracle executes only an immutable copy of validated model output.
// Commands are host constants. External output is never interpreted as commands.
func RunEditOracle(ctx context.Context, id string, before, after EditSnapshot, tools OracleTools) error {
	if !ValidateEdit(id, before, after) {
		return ErrEditContract
	}
	root, err := os.MkdirTemp("", "ivoai-edit-oracle-")
	if err != nil {
		return ErrEditContract
	}
	defer os.RemoveAll(root)
	for name, body := range after.files {
		if name == ".git" || strings.HasPrefix(name, ".git/") {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(name))
		if os.MkdirAll(filepath.Dir(path), 0700) != nil || os.WriteFile(path, body, 0600) != nil {
			return ErrEditContract
		}
	}
	command := []string{"/oracle-go/bin/go", "test", "-count=1", "-json", "./..."}
	if id == "cross_module_refactor" {
		command = []string{"/oracle-node/bin/node", "--input-type=module", "-e", `import assert from "node:assert/strict"; import * as m from "./packages/shared/src/discount.ts"; assert.deepEqual(Object.keys(m),["applyDiscount"]); for(const n of [-100,0,20,80,100,1000]) assert.equal(m.applyDiscount(n),n*9/10); await import("./packages/shop/src/main.ts"); await import("./packages/admin/src/report.ts"); console.log("IVOAI_REFACTOR_ORACLE_OK");`}
	}
	body, err := runEditSandbox(ctx, id, root, tools, command)
	if err != nil {
		return ErrEditContract
	}
	if id == "small_known_file_correction" && !bytes.Contains(body, []byte(`"Action":"pass","Package":"clampfixture/mathpkg","Test":"TestClampOracle"`)) {
		return ErrEditContract
	}
	if id == "cross_module_refactor" && string(body) != "{\"store\":\"shop\",\"result\":90}\n{\"store\":\"admin\",\"result\":72}\nIVOAI_REFACTOR_ORACLE_OK\n" {
		return ErrEditContract
	}
	return nil
}

func runEditSandbox(parent context.Context, id, root string, tools OracleTools, command []string) ([]byte, error) {
	tool := tools.Go
	if id == "cross_module_refactor" {
		tool = tools.Node
	} else if id != "small_known_file_correction" {
		return nil, ErrEditContract
	}
	for _, path := range []string{tools.Bwrap, tool} {
		info, err := os.Stat(path)
		if !filepath.IsAbs(path) || err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			return nil, ErrEditContract
		}
	}
	if filepath.Base(tool) != "go" && id == "small_known_file_correction" || filepath.Base(tool) != "node" && id == "cross_module_refactor" {
		return nil, ErrEditContract
	}
	resolvedTool, err := filepath.EvalSymlinks(tool)
	if err != nil {
		return nil, ErrEditContract
	}
	toolRoot := filepath.Dir(filepath.Dir(resolvedTool))
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--as-pid-1", "--clearenv", "--cap-drop", "ALL"}
	for _, directory := range []string{"/usr/bin", "/usr/lib", "/lib", "/lib64"} {
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			args = append(args, "--ro-bind", directory, directory)
		}
	}
	if id == "small_known_file_correction" {
		args = append(args, "--ro-bind", resolvedTool, "/oracle-go/bin/go")
		for _, name := range []string{"src", "pkg", "lib", "VERSION"} {
			path := filepath.Join(toolRoot, name)
			if _, err := os.Stat(path); err != nil {
				return nil, ErrEditContract
			}
			args = append(args, "--ro-bind", path, "/oracle-go/"+name)
		}
		args = append(args, "--setenv", "GOROOT", "/oracle-go")
	} else {
		// Node's self-contained executable is sufficient for this fixed native
		// TypeScript oracle. Do not mount an arbitrary installation/home root.
		args = append(args, "--ro-bind", resolvedTool, "/oracle-node/bin/node")
	}
	args = append(args, "--ro-bind", root, "/work", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/home/oracle", "--chdir", "/work")
	for _, entry := range []string{"HOME=/home/oracle", "PATH=/oracle-go/bin:/oracle-node/bin:/usr/bin:/bin", "TMPDIR=/tmp", "GOTMPDIR=/tmp", "GOCACHE=/tmp/go-cache", "GOPATH=/tmp/go-path", "GOPROXY=off", "GOSUMDB=off", "GOENV=off", "GOTOOLCHAIN=local", "GOMAXPROCS=2", "CGO_ENABLED=0", "LANG=C.UTF-8"} {
		key, value, _ := strings.Cut(entry, "=")
		args = append(args, "--setenv", key, value)
	}
	args = append(args, "--", "/usr/bin/prlimit", "--as=4294967296", "--cpu=30", "--nofile=128", "--fsize=67108864", "--")
	args = append(args, command...)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tools.Bwrap, args...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	out := &boundedOracleOutput{limit: 1 << 20}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil || out.overflow {
		return nil, ErrEditContract
	}
	return bytes.Clone(out.Bytes()), nil
}

type boundedOracleOutput struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedOracleOutput) Len() int      { return b.buffer.Len() }
func (b *boundedOracleOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *boundedOracleOutput) Write(body []byte) (int, error) {
	n := len(body)
	if n > b.limit-b.Len() {
		body = body[:b.limit-b.Len()]
		b.overflow = true
	}
	_, _ = b.buffer.Write(body)
	return n, nil
}
