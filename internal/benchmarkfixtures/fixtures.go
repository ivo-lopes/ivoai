// Package benchmarkfixtures contains synthetic inputs, structured fact checks
// and explicitly admitted sandboxed edit oracles. It grants no production
// permissions, starts no provider and does not certify economics.
package benchmarkfixtures

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

//go:embed all:data
var inputs embed.FS

//go:embed fixtures.go edit_oracle.go exec_observation.go
var contractSources embed.FS

type Scenario struct {
	ID              string          `json:"id"`
	Label           string          `json:"label"`
	Fixture         string          `json:"fixture"`
	Languages       []string        `json:"languages"`
	RunnerSupported bool            `json:"runner_supported"`
	Blocker         string          `json:"blocker"`
	Task            string          `json:"task"`
	Prompt          string          `json:"prompt"`
	Expected        json.RawMessage `json:"expected"`
	Structural      string          `json:"structural"`
	Execution       string          `json:"execution"`
}

func Catalog() ([]Scenario, error) {
	body, _ := inputs.ReadFile("data/catalog.json")
	var catalog struct {
		Version   int        `json:"schema_version"`
		Scope     string     `json:"scope"`
		Gate      string     `json:"global_economic_gate"`
		Scenarios []Scenario `json:"scenarios"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&catalog) != nil || catalog.Version != 2 || catalog.Gate != "INCOMPLETE" || len(catalog.Scenarios) != 16 {
		return nil, errors.New("invalid benchmark catalog")
	}
	seen := map[string]bool{}
	for _, s := range catalog.Scenarios {
		if s.ID == "" || seen[s.ID] || s.Label == "" || s.RunnerSupported && (s.Blocker != "" || s.Task == "" || s.Prompt == "" || s.Fixture == "" || string(s.Expected) == "null") || !s.RunnerSupported && s.Blocker == "" {
			return nil, errors.New("invalid benchmark scenario")
		}
		seen[s.ID] = true
		if s.RunnerSupported && s.Execution != "read_only" && s.Execution != "workspace_edit" || !s.RunnerSupported && s.Execution != "unavailable" {
			return nil, errors.New("invalid benchmark execution contract")
		}
		if s.Structural != "allowed" && s.Structural != "required" && s.Structural != "forbidden" {
			return nil, errors.New("invalid structural policy")
		}
		if s.Fixture != "" {
			if _, err := inputs.ReadDir("data/projects/" + s.Fixture); err != nil {
				return nil, err
			}
		}
	}
	return catalog.Scenarios, nil
}

func Lookup(id string) (Scenario, bool) {
	cases, err := Catalog()
	if err != nil {
		return Scenario{}, false
	}
	for _, s := range cases {
		if s.ID == id {
			return s, true
		}
	}
	return Scenario{}, false
}

func catalogBytes() []byte  { body, _ := inputs.ReadFile("data/catalog.json"); return body }
func CatalogSHA256() string { sum := sha256.Sum256(catalogBytes()); return hex.EncodeToString(sum[:]) }
func ContractSHA256() string {
	hash := sha256.New()
	for _, name := range []string{"fixtures.go", "edit_oracle.go", "exec_observation.go"} {
		body, _ := contractSources.ReadFile(name)
		hash.Write([]byte(name + "\x00"))
		hash.Write(body)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func ProjectSHA256(name string) (string, error) {
	if name == "" || filepath.Base(name) != name {
		return "", errors.New("invalid fixture")
	}
	prefix := "data/projects/" + name + "/"
	var names []string
	err := fs.WalkDir(inputs, strings.TrimSuffix(prefix, "/"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			names = append(names, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, path := range names {
		body, err := inputs.ReadFile(path)
		if err != nil {
			return "", err
		}
		rel := strings.TrimPrefix(path, prefix)
		rel = strings.TrimSuffix(rel, ".fixture")
		hash.Write([]byte(rel))
		hash.Write([]byte{0})
		hash.Write(body)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// DirectorySHA256 covers all fixture bytes, including non-code formats.
func DirectorySHA256(root string) (string, error) {
	var names []string
	var size int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 1<<20 || len(names) >= 128 {
			return errors.New("unsafe fixture")
		}
		size += info.Size()
		if size > 1<<20 {
			return errors.New("fixture too large")
		}
		names = append(names, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, path := range names {
		body, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(filepath.ToSlash(rel)))
		hash.Write([]byte{0})
		hash.Write(body)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Materialize admits only embedded files into a new empty, caller-owned root.
func Materialize(root, name string) error {
	if _, err := ProjectSHA256(name); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return errors.New("fixture requires an empty private directory")
	}
	prefix := "data/projects/" + name + "/"
	return fs.WalkDir(inputs, strings.TrimSuffix(prefix, "/"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, prefix)
		rel = strings.TrimSuffix(rel, ".fixture")
		if !filepath.IsLocal(rel) {
			return errors.New("invalid fixture path")
		}
		target := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		body, err := inputs.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(body)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	})
}

func Schema(id string) map[string]any {
	s, ok := Lookup(id)
	if !ok || !s.RunnerSupported {
		return nil
	}
	var value any
	if json.Unmarshal(s.Expected, &value) != nil {
		return nil
	}
	var schema func(any) map[string]any
	schema = func(v any) map[string]any {
		switch x := v.(type) {
		case string:
			return map[string]any{"type": "string"}
		case float64:
			return map[string]any{"type": "integer"}
		case []any:
			item := map[string]any{"type": "string"}
			if len(x) > 0 {
				item = schema(x[0])
			}
			return map[string]any{"type": "array", "items": item}
		case map[string]any:
			props := map[string]any{}
			required := []string{}
			for k, value := range x {
				props[k] = schema(value)
				required = append(required, k)
			}
			sort.Strings(required)
			return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
		default:
			return nil
		}
	}
	return schema(value)
}

// CheckAnswer uses an independent exact structured oracle. Duplicate fields,
// unknown fields, different ordering of ordered arrays and trailing JSON fail.
func CheckAnswer(id, text string) (bool, bool) {
	s, ok := Lookup(id)
	if !ok || !s.RunnerSupported {
		return false, false
	}
	var decode func(*json.Decoder) (any, error)
	decode = func(d *json.Decoder) (any, error) {
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				out := map[string]any{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					name, ok := key.(string)
					if !ok {
						return nil, errors.New("invalid key")
					}
					if _, exists := out[name]; exists {
						return nil, errors.New("duplicate key")
					}
					value, err := decode(d)
					if err != nil {
						return nil, err
					}
					out[name] = value
				}
				_, err := d.Token()
				return out, err
			case '[':
				out := []any{}
				for d.More() {
					value, err := decode(d)
					if err != nil {
						return nil, err
					}
					out = append(out, value)
				}
				_, err := d.Token()
				return out, err
			default:
				return nil, errors.New("invalid delimiter")
			}
		}
		return token, nil
	}
	read := func(body string) (any, error) {
		d := json.NewDecoder(strings.NewReader(body))
		d.UseNumber()
		value, err := decode(d)
		if err != nil {
			return nil, err
		}
		if _, err = d.Token(); err != io.EOF {
			return nil, errors.New("trailing content")
		}
		return value, nil
	}
	answer, err := read(text)
	if err != nil {
		return false, false
	}
	expected, err := read(string(s.Expected))
	if err != nil {
		return false, false
	}
	return true, reflect.DeepEqual(answer, expected)
}
