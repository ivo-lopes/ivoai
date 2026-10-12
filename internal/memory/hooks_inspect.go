package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// InspectHooks checks the actual registered wrappers and native executable. It
// never executes a hook or exposes command strings, payloads, or settings values.
func InspectHooks(root string) (bool, []string) {
	receipt, err := readHookReceipt(root)
	if err != nil {
		issues := []string{"ai-memory hook ownership is unknown; existing registrations require explicit review before repair"}
		for _, agent := range []string{"codex", "claude-code"} {
			count := 0
			if safeHookPath(filepath.Join(root, agent)) != nil {
				issues = append(issues, "ai-memory "+agent+" hook directory is unsafe")
				continue
			}
			_ = filepath.WalkDir(filepath.Join(root, agent), func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				if entry.IsDir() || filepath.Ext(path) != ".sh" || entry.Name() == "_lib.sh" {
					return nil
				}
				count++
				issues = append(issues, inspectHookExecutable(path, "ai-memory "+agent+" hook", true)...)
				return nil
			})
			if count == 0 {
				issues = append(issues, "ai-memory "+agent+" hooks are not installed")
			}
		}
		return false, issues
	}
	var issues []string
	if receipt.Disabled {
		issues = append(issues, "ai-memory owned hooks are disabled")
	}
	if hash, err := hookDigest(receipt.Binary); err != nil || hash != receipt.BinarySHA256 {
		issues = append(issues, "ai-memory hook binary is unavailable or its integrity changed")
	}
	issues = append(issues, inspectHookExecutable(receipt.Binary, "ai-memory hook binary", false)...)
	for _, file := range receipt.Files {
		path := filepath.Join(root, file.Path)
		body, _, err := hookBody(path)
		if err != nil || body == nil {
			issues = append(issues, "ai-memory managed hook is missing or unsafe")
			continue
		}
		if !bytes.Equal(body, file.Body) {
			issues = append(issues, "ai-memory managed hook integrity changed")
		}
		issues = append(issues, inspectHookExecutable(path, "ai-memory managed hook", true)...)
	}
	for _, name := range []string{"mkdir", "mktemp", "cat", "rm"} {
		if _, err := exec.LookPath(name); err != nil {
			issues = append(issues, "ai-memory hook dependency is missing from PATH: "+name)
		}
	}
	for _, reg := range receipt.Registrations {
		body, _, err := hookBody(reg.Path)
		if err != nil {
			issues = append(issues, "ai-memory hook registration file is unsafe or unreadable")
			continue
		}
		var doc struct {
			Hooks map[string][]json.RawMessage `json:"hooks"`
		}
		if json.Unmarshal(body, &doc) != nil {
			issues = append(issues, "ai-memory hook registration file is missing or invalid")
			continue
		}
		found := false
		for _, entry := range doc.Hooks[reg.Event] {
			if equalHookJSON(entry, reg.Entry) {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, "ai-memory managed hook registration is missing or stale")
		}
	}
	sort.Strings(issues)
	unique := issues[:0]
	for _, issue := range issues {
		if len(unique) == 0 || unique[len(unique)-1] != issue {
			unique = append(unique, issue)
		}
	}
	return len(unique) == 0, unique
}

func inspectHookExecutable(path, label string, script bool) []string {
	body, mode, err := hookBody(path)
	// Native binaries can exceed the small-file read bound; inspect only their
	// header after the separately bounded streaming integrity check.
	if !script && err != nil && safeHookPath(path) == nil {
		if f, e := os.Open(path); e == nil {
			defer f.Close()
			if info, e := f.Stat(); e == nil && info.Mode().IsRegular() {
				mode = info.Mode()
				body = make([]byte, 256)
				n, _ := f.Read(body)
				body = body[:n]
				err = nil
			}
		}
	}
	if err != nil || body == nil {
		return []string{label + " is not a regular non-symlink file"}
	}
	var issues []string
	if mode&0111 == 0 {
		issues = append(issues, label+" is not executable")
	}
	if !script && !bytes.HasPrefix(body, []byte("#!")) {
		return issues
	}
	first := strings.SplitN(string(body), "\n", 2)[0]
	if !strings.HasPrefix(first, "#!") || strings.Contains(first, "\r") {
		return append(issues, label+" has an invalid shebang")
	}
	words := strings.Fields(strings.TrimPrefix(first, "#!"))
	if len(words) == 0 || !filepath.IsAbs(words[0]) {
		return append(issues, label+" has an invalid interpreter")
	}
	info, err := os.Stat(words[0])
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
		return append(issues, label+" interpreter is unavailable")
	}
	if filepath.Base(words[0]) == "env" {
		words = words[1:]
		split := len(words) > 0 && words[0] == "-S"
		if split {
			words = words[1:]
		}
		if len(words) == 0 || strings.HasPrefix(words[0], "-") || (!split && len(words) > 1) {
			return append(issues, label+" has an invalid env interpreter")
		}
		if _, err := exec.LookPath(words[0]); err != nil {
			issues = append(issues, label+" interpreter is missing from PATH")
		}
	}
	return issues
}

// InstallHookAssets accepts a fresh staging directory extracted from a verified
// archive. Only those exact assets enter the receipt; existing foreign files do
// not become owned merely because they share the destination directory.
func InstallHookAssets(source, root string) error {
	unlock, err := lockHooks(root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := safeHookPath(source); err != nil {
		return err
	}
	var old []hookFile
	previous, _, err := hookBody(filepath.Join(root, hookAssetReceiptName))
	if err != nil {
		return err
	}
	if previous != nil && json.Unmarshal(previous, &old) != nil {
		return errors.New("invalid previous hook asset receipt")
	}
	var assets []hookFile
	files := map[string]hookFile{}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		body, mode, err := hookBody(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(source, path)
		if !safeHookRelative(rel) || strings.HasPrefix(rel, ".ivoai-") {
			return errors.New("invalid verified hook asset path")
		}
		destination := filepath.Join(root, rel)
		current, currentMode, err := hookBody(destination)
		if err != nil {
			return err
		}
		if current != nil && !bytes.Equal(current, body) && !knownHookFile(old, rel, current) {
			return errors.New("existing hook asset has unknown ownership; refusing replacement")
		}
		asset := hookFile{Path: rel, Body: body, Mode: uint32(mode), Before: current, BeforeMode: currentMode, CompareBefore: true}
		assets = append(assets, asset)
		files[destination] = asset
		return nil
	})
	if err != nil {
		return err
	}
	body, _ := json.Marshal(assets)
	files[filepath.Join(root, hookAssetReceiptName)] = hookFile{Body: body, Mode: 0600}
	return commitHookFiles(root, files)
}

func planHookAssetRepair(root string, files map[string]hookFile) error {
	body, _, err := hookBody(filepath.Join(root, hookAssetReceiptName))
	if err != nil {
		return err
	}
	if body == nil {
		return nil
	}
	var assets []hookFile
	if json.Unmarshal(body, &assets) != nil {
		return errors.New("invalid hook asset receipt")
	}
	for _, asset := range assets {
		if !safeHookRelative(asset.Path) {
			return errors.New("invalid hook asset path")
		}
		path := filepath.Join(root, asset.Path)
		current, mode, err := hookBody(path)
		if err != nil {
			return err
		}
		if current != nil && !bytes.Equal(current, asset.Body) {
			return fmt.Errorf("hook source asset was modified; refusing repair")
		}
		asset.Before, asset.BeforeMode, asset.CompareBefore = current, mode, true
		files[path] = asset
	}
	return nil
}
