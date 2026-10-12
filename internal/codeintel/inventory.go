package codeintel

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var generatedBundle = regexp.MustCompile(`(?i)(?:\.[a-f0-9]{8,64}|\.min)\.(?:mjs|cjs|js)$`)

var sourceExtensions = strings.Fields(".go .py .pyi .js .jsx .ts .tsx .mjs .cjs .java .kt .kts .php .r .swift .rs .c .h .cpp .cc .cxx .hpp .hh .rb .cs .scala .ex .exs .sol .ml .mli .zig .dart .clj .cljs .cljc .bb .nix .lua .vue .svelte .astro")

// Inventory uses Git's ignore rules but no hooks, fsmonitor, textconv, global
// configuration or submodule recursion. Nested repositories remain separate.
func Inventory(ctx context.Context, root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/git", "-c", "core.fsmonitor=false", "-c", "core.untrackedCache=false", "-c", "core.hooksPath=/dev/null", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"}
	var output limitedBuffer
	output.max = 4 * 1024 * 1024
	command.Stdout = &output
	command.Stderr = nil
	if e := command.Run(); e != nil {
		return nil, errors.New("code inventory unavailable: no accessible Git working tree")
	}
	dir, e := os.OpenRoot(root)
	if e != nil {
		return nil, e
	}
	defer dir.Close()
	var files []string
	for _, name := range strings.Split(output.String(), "\x00") {
		if name == "" || !filepath.IsLocal(name) || filepath.Clean(name) != name {
			continue
		}
		if generatedBundle.MatchString(filepath.Base(name)) {
			continue
		}
		if !slices.Contains(sourceExtensions, strings.ToLower(filepath.Ext(name))) && name != "go.mod" {
			continue
		}
		parts := strings.Split(name, string(filepath.Separator))
		excluded := false
		for i, part := range parts {
			if strings.HasPrefix(part, ".") || slices.Contains([]string{"node_modules", "vendor", "dist", "build", "target", "venv", "__pycache__"}, part) {
				excluded = true
				break
			}
			if i < len(parts)-1 {
				if _, err := dir.Stat(filepath.Join(filepath.Join(parts[:i+1]...), ".git")); err == nil {
					excluded = true
					break
				}
			}
		}
		if excluded {
			continue
		}
		st, err := dir.Lstat(name)
		if err != nil || !st.Mode().IsRegular() || st.Size() > MaxFileBytes {
			continue
		}
		files = append(files, name)
		if len(files) > MaxFiles {
			return nil, errors.New("code inventory exceeds file budget")
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, errors.New("output budget exceeded")
	}
	return b.Buffer.Write(p)
}
