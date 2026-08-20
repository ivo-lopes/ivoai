package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/pelletier/go-toml/v2"
)

const MarkerName = ".ivoai.toml"

type Marker struct {
	Version        int      `toml:"version"`
	ID             string   `toml:"id"`
	ContextSources []string `toml:"context_sources"`
}

func Init(dir string) (Marker, error) {
	root, err := gitRoot(dir)
	if err != nil {
		return Marker{}, errors.New("ivoai project init must run inside a Git repository")
	}
	id := "project:" + shortHash(filepath.Clean(root))
	marker := Marker{Version: 1, ID: id, ContextSources: []string{"."}}
	b, err := toml.Marshal(marker)
	if err != nil {
		return Marker{}, err
	}
	path := filepath.Join(root, MarkerName)
	if existing, readErr := os.ReadFile(path); readErr == nil {
		var current Marker
		if toml.Unmarshal(existing, &current) == nil && current.Version == 1 {
			return current, nil
		}
		return Marker{}, fmt.Errorf("refusing to overwrite invalid %s", path)
	}
	if err := platform.AtomicWritePrivate(b, path); err != nil {
		return Marker{}, err
	}
	return marker, nil
}

func Identity(dir string) string {
	current := filepath.Clean(dir)
	for {
		b, err := os.ReadFile(filepath.Join(current, MarkerName))
		if err == nil {
			var m Marker
			if toml.Unmarshal(b, &m) == nil && m.ID != "" {
				return m.ID
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	host, _ := os.Hostname()
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		host = "unknown"
	}
	return "host:" + host
}

func gitRoot(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
