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
	Server *ClientCredential `json:"server,omitempty"`
}
type Store struct{ Path string }

func (s Store) Load() (Data, error) {
	before, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Data{}, nil
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
	return data, nil
}

func (s Store) Save(data Data) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode secrets: %w", err)
	}
	b = append(b, '\n')
	return platform.AtomicWritePrivate(b, s.Path)
}

func (s Store) RemoveServer() error {
	data, err := s.Load()
	if err != nil {
		return err
	}
	data.Server = nil
	if data.Server == nil {
		// Keep an empty, private store so permission checks remain deterministic.
		return s.Save(data)
	}
	return nil
}
