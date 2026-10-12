package memory

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"golang.org/x/sys/unix"
)

const hookReceiptName = ".ivoai-hooks.json"
const hookAssetReceiptName = ".ivoai-hook-assets.json"

type hookFile struct {
	Path string `json:"path"`
	Body []byte `json:"body"`
	Mode uint32 `json:"mode"`
}
type hookRegistration struct {
	Path  string          `json:"path"`
	Event string          `json:"event"`
	Entry json.RawMessage `json:"entry"`
}
type hookReceipt struct {
	Schema        int                `json:"schema"`
	Binary        string             `json:"binary"`
	BinarySHA256  string             `json:"binary_sha256"`
	Files         []hookFile         `json:"files"`
	Registrations []hookRegistration `json:"registrations"`
	Disabled      bool               `json:"disabled,omitempty"`
}

// HookRegistrationPaths honors the same explicit provider homes as ai-memory.
// Neither this function nor inspection opens vendor authentication stores.
func HookRegistrationPaths() map[string]string {
	home, _ := os.UserHomeDir()
	codex := os.Getenv("CODEX_HOME")
	if codex == "" {
		codex = filepath.Join(home, ".codex")
	}
	claude := os.Getenv("CLAUDE_CONFIG_DIR")
	if claude == "" {
		claude = filepath.Join(home, ".claude")
	}
	return map[string]string{"codex": filepath.Join(codex, "hooks.json"), "claude-code": filepath.Join(claude, "settings.json")}
}

func hookDigest(path string) (string, error) {
	if err := safeHookPath(path); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<20 {
		return "", errors.New("hook executable is not a bounded regular file")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func safeHookPath(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("hook path must be absolute")
	}
	for p := filepath.Clean(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("hook path is unreadable")
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("hook path contains a symlink")
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}
func hookBody(path string) ([]byte, os.FileMode, error) {
	if err := safeHookPath(path); err != nil {
		return nil, 0, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("hook path is not a regular file")
	}
	body, err := platform.ReadRegularFile(path, 8<<20)
	return body, info.Mode().Perm(), err
}
func readHookReceipt(root string) (hookReceipt, error) {
	var r hookReceipt
	body, _, err := hookBody(filepath.Join(root, hookReceiptName))
	if err != nil {
		return r, err
	}
	if len(body) == 0 {
		return r, errors.New("no IVOAI hook ownership receipt; existing hooks require explicit adoption")
	}
	if json.Unmarshal(body, &r) != nil || r.Schema != 1 {
		return r, errors.New("invalid IVOAI hook receipt")
	}
	allowed := HookRegistrationPaths()
	for _, reg := range r.Registrations {
		if reg.Path != allowed["codex"] && reg.Path != allowed["claude-code"] {
			return r, errors.New("hook registration home changed; refusing repair")
		}
	}
	for _, file := range r.Files {
		if !safeHookRelative(file.Path) {
			return r, errors.New("invalid hook receipt path")
		}
	}
	return r, nil
}
func safeHookRelative(path string) bool {
	return path != "." && !filepath.IsAbs(path) && filepath.Clean(path) == path && path != ".." && !strings.HasPrefix(path, ".."+string(filepath.Separator))
}
func shellHookQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }

func hookWrapper(command, id, root string) []byte {
	// mkdir supplies an atomic per-hook failure latch. Output from a failing
	// optional command is discarded; one fixed diagnostic contains no payload.
	return []byte("#!/bin/sh\n# IVOAI managed optional lifecycle hook.\numask 077\nstate=${IVOAI_HOOK_SESSION_DIR:-" + shellHookQuote(filepath.Join(root, ".ivoai-hook-session")) + "}\n[ -d \"$state/disabled\" ] && exit 0\nmkdir -p \"$state\" 2>/dev/null || exit 0\nout=$(mktemp \"$state/output.XXXXXXXX\") || exit 0\ntrap 'rm -f \"$out\"' EXIT HUP INT TERM\n( " + command + " ) >\"$out\" 2>/dev/null\nstatus=$?\nif [ \"$status\" -eq 0 ]; then\n cat \"$out\"\nelse\n if mkdir \"$state/disabled\" 2>/dev/null; then\n  printf '%s\\n' 'ivoai: optional memory hook disabled after failure; run ivoai doctor --repair-hooks.' >&2\n fi\nfi\nexit 0\n")
}

// applyHookConfiguration receives only output rendered by the pinned binary in
// an isolated home. Existing registrations are never claimed by a name match.
func applyHookConfiguration(root, binary string, plans map[string][]byte) error {
	unlock, err := lockHooks(root)
	if err != nil {
		return err
	}
	defer unlock()
	digest, err := hookDigest(binary)
	if err != nil {
		return err
	}
	old, oldErr := readHookReceipt(root)
	if oldErr != nil {
		if b, _, e := hookBody(filepath.Join(root, hookReceiptName)); e != nil || len(b) > 0 {
			return oldErr
		}
	}
	receipt := hookReceipt{Schema: 1, Binary: binary, BinarySHA256: digest}
	files := map[string]hookFile{}
	paths := make([]string, 0, len(plans))
	for path := range plans {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		allowed := HookRegistrationPaths()
		if path != allowed["codex"] && path != allowed["claude-code"] {
			return errors.New("unsupported hook registration destination")
		}
		var document map[string]json.RawMessage
		if json.Unmarshal(plans[path], &document) != nil {
			return errors.New("invalid rendered hook configuration")
		}
		var events map[string][]json.RawMessage
		if json.Unmarshal(document["hooks"], &events) != nil || len(events) == 0 {
			return errors.New("renderer returned no hooks")
		}
		names := make([]string, 0, len(events))
		for event := range events {
			names = append(names, event)
		}
		sort.Strings(names)
		for _, event := range names {
			for idx, raw := range events[event] {
				var entry map[string]json.RawMessage
				if json.Unmarshal(raw, &entry) != nil {
					return errors.New("invalid rendered hook entry")
				}
				var handlers []map[string]json.RawMessage
				if json.Unmarshal(entry["hooks"], &handlers) != nil {
					return errors.New("invalid rendered hook handlers")
				}
				for h, handler := range handlers {
					var command string
					if json.Unmarshal(handler["command"], &command) != nil || strings.TrimSpace(command) == "" {
						return errors.New("invalid rendered hook command")
					}
					// Exec-form handlers are converted to safely quoted argv before wrapping.
					if args, ok := handler["args"]; ok {
						var argv []string
						if json.Unmarshal(args, &argv) != nil {
							return errors.New("invalid rendered hook arguments")
						}
						command = shellHookQuote(command)
						for _, arg := range argv {
							command += " " + shellHookQuote(arg)
						}
						delete(handler, "args")
					}
					sum := sha256.Sum256([]byte(path + "\x00" + event + fmt.Sprint(idx, "/", h)))
					id := hex.EncodeToString(sum[:12])
					rel := filepath.Join(".ivoai-managed", id+".sh")
					body := hookWrapper(command, id, root)
					file := hookFile{Path: rel, Body: body, Mode: 0700}
					if current, _, e := hookBody(filepath.Join(root, rel)); e != nil {
						return e
					} else if current != nil && !knownHookFile(old.Files, rel, current) && !bytes.Equal(current, body) {
						return errors.New("hook wrapper was modified; refusing replacement")
					}
					receipt.Files = append(receipt.Files, file)
					files[filepath.Join(root, rel)] = file
					handler["command"], _ = json.Marshal(shellHookQuote(filepath.Join(root, rel)))
				}
				entry["hooks"], _ = json.Marshal(handlers)
				updated, _ := json.Marshal(entry)
				receipt.Registrations = append(receipt.Registrations, hookRegistration{Path: path, Event: event, Entry: updated})
			}
		}
	}
	if err := planHookRegistrations(root, receipt, old, &files); err != nil {
		return err
	}
	body, _ := json.MarshalIndent(receipt, "", "  ")
	files[filepath.Join(root, hookReceiptName)] = hookFile{Body: body, Mode: 0600}
	return commitHookFiles(root, files)
}
func knownHookFile(files []hookFile, path string, body []byte) bool {
	for _, file := range files {
		if file.Path == path && bytes.Equal(file.Body, body) {
			return true
		}
	}
	return false
}
func equalHookJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	aa, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return bytes.Equal(aa, bb)
}

func planHookRegistrations(root string, next, old hookReceipt, files *map[string]hookFile) error {
	paths := map[string]bool{}
	for _, reg := range next.Registrations {
		paths[reg.Path] = true
	}
	for path := range paths {
		body, mode, err := hookBody(path)
		if err != nil {
			return err
		}
		doc := map[string]json.RawMessage{}
		if len(body) > 0 && (json.Unmarshal(body, &doc) != nil || doc == nil) {
			return errors.New("hook settings are invalid; no changes made")
		}
		events := map[string][]json.RawMessage{}
		if raw, ok := doc["hooks"]; ok && (json.Unmarshal(raw, &events) != nil || events == nil) {
			return errors.New("hook settings have an invalid events object")
		}
		for _, reg := range next.Registrations {
			if reg.Path != path {
				continue
			}
			entries := events[reg.Event]
			found := false
			for _, entry := range entries {
				if equalHookJSON(entry, reg.Entry) {
					found = true
					break
				}
			}
			if found {
				continue
			}
			// Replace only the exact entry recorded by our preceding transaction.
			kept := make([]json.RawMessage, 0, len(entries)+1)
			for _, entry := range entries {
				owned := false
				for _, previous := range old.Registrations {
					if previous.Path == path && previous.Event == reg.Event && equalHookJSON(entry, previous.Entry) {
						owned = true
						break
					}
				}
				if !owned {
					for _, file := range next.Files {
						if referencesHookWrapper(entry, filepath.Join(root, file.Path)) {
							return errors.New("managed hook registration was modified; refusing replacement")
						}
					}
					kept = append(kept, entry)
				}
			}
			events[reg.Event] = append(kept, reg.Entry)
		}
		doc["hooks"], _ = json.Marshal(events)
		out, _ := json.MarshalIndent(doc, "", "  ")
		if mode == 0 {
			mode = 0600
		}
		(*files)[path] = hookFile{Body: out, Mode: uint32(mode)}
	}
	return nil
}

// RepairHooks restores receipt-backed missing files and executable permissions.
// Modified content and symlinks require explicit operator review, never adoption.
func RepairHooks(root string) error { return repairHooks(root, "") }

// RepairHooksForBinary binds the receipt to current managed component state.
func RepairHooksForBinary(root, binary string) error { return repairHooks(root, binary) }

func repairHooks(root, expectedBinary string) error {
	unlock, err := lockHooks(root)
	if err != nil {
		return err
	}
	defer unlock()
	receipt, err := readHookReceipt(root)
	if err != nil {
		return err
	}
	if expectedBinary != "" && receipt.Binary != expectedBinary {
		return errors.New("hook receipt does not match the current managed binary; reconfigure hooks explicitly")
	}
	hash, err := hookDigest(receipt.Binary)
	if err != nil || hash != receipt.BinarySHA256 {
		return errors.New("managed hook binary changed or is unavailable; update the managed component first")
	}
	if issues := inspectHookExecutable(receipt.Binary, "ai-memory hook binary", false); len(issues) != 0 {
		return errors.New("managed hook binary requires component repair before hook repair")
	}
	files := map[string]hookFile{}
	for _, file := range receipt.Files {
		path := filepath.Join(root, file.Path)
		body, _, err := hookBody(path)
		if err != nil {
			return err
		}
		if body != nil && !bytes.Equal(body, file.Body) {
			return errors.New("hook content was modified; refusing repair")
		}
		files[path] = file
	}
	if err := planHookAssetRepair(root, files); err != nil {
		return err
	}
	if err := planHookRegistrations(root, receipt, receipt, &files); err != nil {
		return err
	}
	receipt.Disabled = false
	body, _ := json.MarshalIndent(receipt, "", "  ")
	files[filepath.Join(root, hookReceiptName)] = hookFile{Body: body, Mode: 0600}
	return commitHookFiles(root, files)
}

// commitHookFiles prepares every backup before the first write and rolls back
// completed writes on failure. Shared settings are retained privately, never logged.
func commitHookFiles(root string, files map[string]hookFile) error {
	return commitHookFilesUsing(root, files, platform.AtomicWriteFile)
}

func commitHookFilesUsing(root string, files map[string]hookFile, write func([]byte, string, os.FileMode) error) error {
	type snapshot struct {
		path string
		body []byte
		mode os.FileMode
	}
	var snapshots []snapshot
	keys := make([]string, 0, len(files))
	for path := range files {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		body, mode, err := hookBody(path)
		if err != nil {
			return err
		}
		file := files[path]
		if bytes.Equal(body, file.Body) && mode == os.FileMode(file.Mode) {
			continue
		}
		snapshots = append(snapshots, snapshot{path, body, mode})
	}
	if len(snapshots) == 0 {
		return nil
	}
	if err := platform.EnsurePrivateDir(root); err != nil {
		return err
	}
	backup, err := os.MkdirTemp(root, ".ivoai-hook-backup-")
	if err != nil {
		return err
	}
	for i, s := range snapshots {
		if s.body != nil {
			if err := platform.AtomicWritePrivate(s.body, filepath.Join(backup, fmt.Sprintf("%d", i))); err != nil {
				return err
			}
		}
	}
	// Retain a non-secret index for explicit rollback; settings snapshots remain 0600.
	type backupEntry struct {
		Path    string `json:"path"`
		Mode    uint32 `json:"mode"`
		Existed bool   `json:"existed"`
	}
	backupPaths := make([]backupEntry, len(snapshots))
	for i, s := range snapshots {
		backupPaths[i] = backupEntry{s.path, uint32(s.mode), s.body != nil}
	}
	index, _ := json.Marshal(backupPaths)
	if err := platform.AtomicWritePrivate(index, filepath.Join(backup, "paths.json")); err != nil {
		return err
	}
	for i, s := range snapshots {
		current, mode, err := hookBody(s.path)
		if err == nil && (!bytes.Equal(current, s.body) || mode != s.mode) {
			err = errors.New("hook configuration changed concurrently")
		}
		rollbackFrom := i - 1
		if err == nil {
			rollbackFrom = i
			file := files[s.path]
			err = write(file.Body, s.path, os.FileMode(file.Mode))
		}
		if err != nil {
			var rollback error
			for j := rollbackFrom; j >= 0; j-- {
				previous := snapshots[j]
				var e error
				if previous.body == nil {
					e = os.Remove(previous.path)
					if errors.Is(e, os.ErrNotExist) {
						e = nil
					}
				} else {
					e = write(previous.body, previous.path, previous.mode)
				}
				rollback = errors.Join(rollback, e)
			}
			return errors.Join(errors.New("hook transaction failed; original files restored where possible"), err, rollback)
		}
	}
	return nil
}

// Serialize managed installers and repairs, while refusing a replaced lock path.
func lockHooks(root string) (func(), error) {
	if err := safeHookPath(root); err != nil {
		return nil, err
	}
	if err := platform.EnsurePrivateDir(root); err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(root, ".ivoai-hooks.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unix.Close(fd)
		return nil, errors.New("another hook transaction is active")
	}
	return func() { _ = unix.Flock(fd, unix.LOCK_UN); _ = unix.Close(fd) }, nil
}

// Recognize only an exact generated wrapper command (or an explicit argument
// added to it). This rejects modified entries without claiming foreign commands.
func referencesHookWrapper(raw []byte, path string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(value any) bool {
		switch v := value.(type) {
		case map[string]any:
			if command, ok := v["command"].(string); ok {
				expected := shellHookQuote(path)
				if command == expected || command == path || strings.HasPrefix(command, expected+" ") {
					return true
				}
			}
			for _, child := range v {
				if walk(child) {
					return true
				}
			}
		case []any:
			for _, child := range v {
				if walk(child) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}

// disableOwnedHooks removes exact receipt entries without invoking upstream's
// broad name-based uninstaller. Assets and receipt remain available for repair.
func disableOwnedHooks(root string) error {
	unlock, err := lockHooks(root)
	if err != nil {
		return err
	}
	defer unlock()
	receipt, err := readHookReceipt(root)
	if err != nil {
		return err
	}
	files := map[string]hookFile{}
	paths := map[string]bool{}
	for _, reg := range receipt.Registrations {
		paths[reg.Path] = true
	}
	for path := range paths {
		body, mode, err := hookBody(path)
		if err != nil {
			return err
		}
		if body == nil {
			continue
		}
		var doc map[string]json.RawMessage
		if json.Unmarshal(body, &doc) != nil || doc == nil {
			return errors.New("invalid hook configuration; refusing disable")
		}
		events := map[string][]json.RawMessage{}
		if raw, ok := doc["hooks"]; ok && (json.Unmarshal(raw, &events) != nil || events == nil) {
			return errors.New("invalid hook events; refusing disable")
		}
		for event, entries := range events {
			kept := make([]json.RawMessage, 0, len(entries))
			for _, entry := range entries {
				owned := false
				for _, reg := range receipt.Registrations {
					if reg.Path == path && reg.Event == event && equalHookJSON(entry, reg.Entry) {
						owned = true
						break
					}
				}
				if !owned {
					kept = append(kept, entry)
				}
			}
			events[event] = kept
		}
		doc["hooks"], _ = json.Marshal(events)
		updated, _ := json.MarshalIndent(doc, "", "  ")
		files[path] = hookFile{Body: updated, Mode: uint32(mode)}
	}
	receipt.Disabled = true
	body, _ := json.MarshalIndent(receipt, "", "  ")
	files[filepath.Join(root, hookReceiptName)] = hookFile{Body: body, Mode: 0600}
	return commitHookFiles(root, files)
}
