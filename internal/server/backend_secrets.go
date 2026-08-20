package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const backendSecretBytes = 32

var backendSecretFiles = map[string]string{
	"qdrant.env":     "QDRANT__SERVICE__API_KEY",
	"embeddings.env": "API_KEY",
	"memory.env":     "AI_MEMORY_AUTH_TOKEN",
}

// EnsureBackendSecrets creates independent, private credentials for each
// loopback dependency. Existing secrets are validated and never rotated by an
// idempotent setup.
func EnsureBackendSecrets(layout Layout) error {
	if err := os.MkdirAll(layout.SecretsDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(layout.SecretsDir, 0o700); err != nil {
		return err
	}
	for name, variable := range backendSecretFiles {
		path := filepath.Join(layout.SecretsDir, name)
		if err := ensureBackendSecret(path, variable); err != nil {
			return err
		}
	}
	return nil
}

func ensureBackendSecret(path, variable string) error {
	if _, err := os.Lstat(path); err == nil {
		data, err := readBackendSecretFile(path)
		if err != nil {
			return err
		}
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), variable+"="))
		decoded, decodeErr := hex.DecodeString(value)
		if !strings.HasPrefix(strings.TrimSpace(string(data)), variable+"=") || decodeErr != nil || len(decoded) != backendSecretBytes {
			return fmt.Errorf("backend secret %s is malformed", path)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	secret := make([]byte, backendSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("generate backend credential: %w", err)
	}
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("open backend credential")
	}
	defer file.Close()
	if _, err := fmt.Fprintf(file, "%s=%s\n", variable, hex.EncodeToString(secret)); err != nil {
		return err
	}
	return file.Sync()
}

// LoadBackendSecret reads one generated variable without ever logging it.
func LoadBackendSecret(layout Layout, filename, variable string) (string, error) {
	path := filepath.Join(layout.SecretsDir, filename)
	data, err := readBackendSecretFile(path)
	if err != nil {
		return "", fmt.Errorf("backend secret %s is unavailable or unsafe: %w", filename, err)
	}
	line := strings.TrimSpace(string(data))
	value, found := strings.CutPrefix(line, variable+"=")
	decoded, decodeErr := hex.DecodeString(value)
	if !found || decodeErr != nil || len(decoded) != backendSecretBytes {
		return "", fmt.Errorf("backend secret %s is malformed", filename)
	}
	return value, nil
}

func readBackendSecretFile(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open backend credential")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() > 4096 {
		return nil, errors.New("backend credential must be a bounded regular 0600 file")
	}
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		return nil, err
	}
	return data, nil
}
