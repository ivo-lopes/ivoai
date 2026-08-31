package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

type ClientCredential struct {
	Token     string    `json:"token"`
	ClientID  string    `json:"client_id"`
	Scopes    []string  `json:"scopes"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

type Data struct {
	Schema  int                         `json:"schema,omitempty"`
	Server  *ClientCredential           `json:"server,omitempty"`
	Servers map[string]ClientCredential `json:"servers,omitempty"`
}
type Store struct{ Path string }

const SchemaVersion = 2
const legacyServerID = "srv_legacy_default"

func (s Store) Load() (Data, error) {
	before, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Data{Schema: SchemaVersion, Servers: map[string]ClientCredential{}}, nil
	}
	if err != nil {
		return Data{}, fmt.Errorf("inspect secret store: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return Data{}, errors.New("secret store must be a regular, non-symlink file")
	}
	if before.Mode().Perm()&0o077 != 0 {
		return Data{}, fmt.Errorf("secret store %s must have mode 0600", s.Path)
	}
	file, err := os.Open(s.Path)
	if err != nil {
		return Data{}, fmt.Errorf("open secret store: %w", err)
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return Data{}, errors.New("secret store changed while being opened")
	}
	if after.Size() > 1<<20 {
		return Data{}, errors.New("secret store exceeds size limit")
	}
	b, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return Data{}, fmt.Errorf("read secret store: %w", err)
	}
	if len(b) > 1<<20 {
		return Data{}, errors.New("secret store exceeds size limit")
	}
	var data Data
	if err := json.Unmarshal(b, &data); err != nil {
		return Data{}, fmt.Errorf("parse secret store: %w", err)
	}
	if data.Schema != 0 && data.Schema != SchemaVersion {
		return Data{}, fmt.Errorf("unsupported secret store schema %d", data.Schema)
	}
	if data.Servers == nil {
		data.Servers = map[string]ClientCredential{}
	}
	if data.Server != nil {
		if _, exists := data.Servers[legacyServerID]; !exists {
			data.Servers[legacyServerID] = *data.Server
		}
	}
	data.Schema = SchemaVersion
	return data, nil
}

func (s Store) Save(data Data) error {
	legacyInput := data.Schema == 0 && data.Server != nil && len(data.Servers) == 0
	data.Schema = SchemaVersion
	if data.Servers == nil {
		data.Servers = map[string]ClientCredential{}
	}
	if legacyInput {
		if _, exists := data.Servers[legacyServerID]; !exists {
			data.Servers[legacyServerID] = *data.Server
		}
	}
	// Keep the published v0.5 field as a rollback bridge. It mirrors only the
	// legacy/default credential and never aliases one server's token to another.
	if credential, ok := data.Servers[legacyServerID]; ok {
		copy := credential
		data.Server = &copy
	} else {
		data.Server = nil
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}
	b = append(b, '\n')
	return platform.AtomicWritePrivate(b, s.Path)
}

func (s Store) RemoveServer() error {
	return s.Remove(legacyServerID)
}

func (s Store) Set(serverID string, credential ClientCredential) error {
	if err := validateServerID(serverID); err != nil {
		return err
	}
	data, err := s.Load()
	if err != nil {
		return err
	}
	data.Servers[serverID] = credential
	return s.Save(data)
}

func (s Store) Get(serverID string) (ClientCredential, bool, error) {
	if err := validateServerID(serverID); err != nil {
		return ClientCredential{}, false, err
	}
	data, err := s.Load()
	if err != nil {
		return ClientCredential{}, false, err
	}
	credential, ok := data.Servers[serverID]
	return credential, ok, nil
}

func (s Store) Remove(serverID string) error {
	if err := validateServerID(serverID); err != nil {
		return err
	}
	data, err := s.Load()
	if err != nil {
		return err
	}
	delete(data.Servers, serverID)
	if serverID == legacyServerID {
		data.Server = nil
	}
	return s.Save(data)
}

func validateServerID(value string) error {
	if len(value) < 1 || len(value) > 128 {
		return errors.New("invalid server credential identity")
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return errors.New("invalid server credential identity")
		}
	}
	return nil
}
