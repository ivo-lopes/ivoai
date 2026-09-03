package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

const docsConfigFile = "docs.json"

type DocsConfig struct {
	ListenAddress string `json:"listen_address"`
}

func DefaultDocsConfig() DocsConfig       { return DocsConfig{ListenAddress: "0.0.0.0:7780"} }
func DocsConfigPath(layout Layout) string { return filepath.Join(layout.ConfigDir, docsConfigFile) }

func (c DocsConfig) Validate() error {
	host, portText, err := net.SplitHostPort(c.ListenAddress)
	if err != nil || host == "" {
		return errors.New("docs listen address must be an explicit IP host:port")
	}
	if net.ParseIP(host) == nil {
		return errors.New("docs listen host must be an IP address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("docs listen port must be between 1 and 65535")
	}
	return nil
}

func EnsureDocsConfig(layout Layout) error {
	path := DocsConfigPath(layout)
	if _, err := os.Lstat(path); err == nil {
		_, loadErr := LoadDocsConfig(layout)
		return loadErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return SaveDocsConfig(layout, DefaultDocsConfig())
}

func LoadDocsConfig(layout Layout) (DocsConfig, error) {
	value := DefaultDocsConfig()
	path := DocsConfigPath(layout)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	permissions := info.Mode().Perm()
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || (permissions != 0o600 && permissions != 0o640) {
		return value, errors.New("docs configuration must be a regular 0600 or root-owned 0640 file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return value, fmt.Errorf("decode docs configuration: %w", err)
	}
	return value, value.Validate()
}

func SaveDocsConfig(layout Layout, value DocsConfig) error {
	if err := value.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeManagedFile(DocsConfigPath(layout), append(data, '\n'), 0o600)
}
