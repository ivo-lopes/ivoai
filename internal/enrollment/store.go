// Package enrollment implements one-time enrollment and scoped client tokens.
package enrollment

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type Scope string

const (
	ScopeContextRead   Scope = "context:read"
	ScopeMemoryRead    Scope = "memory:read"
	ScopeMemoryWrite   Scope = "memory:write"
	ScopeStatusRead    Scope = "status:read"
	ScopeDoctorRead    Scope = "doctor:read"
	ScopeConnectorRead Scope = "connector:read"
)

var DefaultClientScopes = []Scope{ScopeContextRead, ScopeMemoryRead, ScopeMemoryWrite, ScopeStatusRead, ScopeDoctorRead, ScopeConnectorRead}

type Enrollment struct {
	ID         string    `json:"id"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	ConsumedAt time.Time `json:"consumed_at,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitempty"`
	Scopes     []Scope   `json:"scopes"`
}

type CreatedEnrollment struct {
	Enrollment
	Code string `json:"code"`
}

type ClientCredential struct {
	ClientID string  `json:"client_id"`
	Token    string  `json:"token"`
	Scopes   []Scope `json:"scopes"`
}

type Principal struct {
	ClientID string
	Name     string
	Scopes   []Scope
}

type storedEnrollment struct {
	Enrollment
	CodeHash string `json:"code_hash"`
}

type storedClient struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	TokenHash string    `json:"token_hash"`
	Scopes    []Scope   `json:"scopes"`
	CreatedAt time.Time `json:"created_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
}

type state struct {
	Enrollments map[string]storedEnrollment `json:"enrollments"`
	Clients     map[string]storedClient     `json:"clients"`
}

// Store persists only hashes of bearer material in an owner-only state file.
type Store struct {
	Path   string
	Clock  func() time.Time
	Random io.Reader
	mu     sync.RWMutex
}

func NewStore(path string) *Store { return &Store{Path: path, Clock: time.Now, Random: rand.Reader} }

func (s *Store) now() time.Time {
	if s.Clock == nil {
		return time.Now().UTC()
	}
	return s.Clock().UTC()
}

func (s *Store) randomBytes(size int) ([]byte, error) {
	reader := s.Random
	if reader == nil {
		reader = rand.Reader
	}
	value := make([]byte, size)
	if _, err := io.ReadFull(reader, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (s *Store) load() (state, error) {
	current := state{Enrollments: make(map[string]storedEnrollment), Clients: make(map[string]storedClient)}
	info, err := os.Lstat(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return current, nil
	}
	if err != nil {
		return current, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return current, errors.New("enrollment state must be a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return current, errors.New("enrollment state permissions must be 0600")
	}
	fd, err := unix.Open(s.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return current, err
	}
	file := os.NewFile(uintptr(fd), s.Path)
	if file == nil {
		_ = unix.Close(fd)
		return current, errors.New("open enrollment state")
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || !after.Mode().IsRegular() {
		return current, errors.New("enrollment state changed while being opened")
	}
	if after.Size() > 16<<20 {
		return current, errors.New("enrollment state exceeds size limit")
	}
	limited := &io.LimitedReader{R: file, N: (16 << 20) + 1}
	if err := json.NewDecoder(limited).Decode(&current); err != nil {
		return current, fmt.Errorf("decode enrollment state: %w", err)
	}
	if limited.N <= 0 {
		return current, errors.New("enrollment state exceeds size limit")
	}
	if current.Enrollments == nil {
		current.Enrollments = make(map[string]storedEnrollment)
	}
	if current.Clients == nil {
		current.Clients = make(map[string]storedClient)
	}
	return current, nil
}

func (s *Store) save(current state) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".enrollment-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	encoder := json.NewEncoder(temp)
	if err := encoder.Encode(current); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if info, err := os.Lstat(s.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace symlink enrollment state")
	}
	return os.Rename(name, s.Path)
}

func hash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (s *Store) lockState() (func(), error) {
	return s.lockStateMode(syscall.LOCK_EX)
}

func (s *Store) lockStateShared() (func(), error) {
	return s.lockStateMode(syscall.LOCK_SH)
}

func (s *Store) lockStateMode(mode int) (func(), error) {
	dir := filepath.Dir(s.Path)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("refusing symlink enrollment state directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	lockPath := s.Path + ".lock"
	if info, err := os.Lstat(lockPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("refusing symlink enrollment lock")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), lockPath)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open enrollment lock")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("enrollment lock must be a regular file")
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), mode); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock enrollment state: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func (s *Store) Create(ttl time.Duration, scopes []Scope) (CreatedEnrollment, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		return CreatedEnrollment{}, errors.New("enrollment TTL must be between zero and 24 hours")
	}
	if len(scopes) == 0 {
		scopes = append([]Scope(nil), DefaultClientScopes...)
	}
	idBytes, err := s.randomBytes(8)
	if err != nil {
		return CreatedEnrollment{}, err
	}
	secret, err := s.randomBytes(32)
	if err != nil {
		return CreatedEnrollment{}, err
	}
	id := hex.EncodeToString(idBytes)
	code := "ivoai-enroll_" + id + "_" + base64.RawURLEncoding.EncodeToString(secret)
	now := s.now()
	created := CreatedEnrollment{Enrollment: Enrollment{ID: id, CreatedAt: now, ExpiresAt: now.Add(ttl), Scopes: append([]Scope(nil), scopes...)}, Code: code}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockState()
	if err != nil {
		return CreatedEnrollment{}, err
	}
	defer unlock()
	current, err := s.load()
	if err != nil {
		return CreatedEnrollment{}, err
	}
	current.Enrollments[id] = storedEnrollment{Enrollment: created.Enrollment, CodeHash: hash(code)}
	if err := s.save(current); err != nil {
		return CreatedEnrollment{}, err
	}
	return created, nil
}

func codeID(code string) (string, bool) {
	const prefix = "ivoai-enroll_"
	if !strings.HasPrefix(code, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(code, prefix)
	if len(remainder) <= 17 || remainder[16] != '_' {
		return "", false
	}
	id := remainder[:16]
	if _, err := hex.DecodeString(id); err != nil {
		return "", false
	}
	return id, true
}

func (s *Store) Consume(code, clientName string) (ClientCredential, error) {
	return s.ConsumeScoped(code, clientName, nil)
}

// ConsumeScoped atomically consumes a code and grants only the requested
// subset. A nil request preserves the enrollment's full scope set.
func (s *Store) ConsumeScoped(code, clientName string, requested []Scope) (ClientCredential, error) {
	if strings.TrimSpace(clientName) == "" || len(clientName) > 128 {
		return ClientCredential{}, errors.New("client name is required")
	}
	id, valid := codeID(code)
	if !valid {
		return ClientCredential{}, errors.New("invalid or expired enrollment code")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockState()
	if err != nil {
		return ClientCredential{}, err
	}
	defer unlock()
	current, err := s.load()
	if err != nil {
		return ClientCredential{}, err
	}
	record, found := current.Enrollments[id]
	providedHash := hash(code)
	if !found || subtle.ConstantTimeCompare([]byte(record.CodeHash), []byte(providedHash)) != 1 || !record.ConsumedAt.IsZero() || !record.RevokedAt.IsZero() || !s.now().Before(record.ExpiresAt) {
		return ClientCredential{}, errors.New("invalid or expired enrollment code")
	}
	granted := append([]Scope(nil), record.Scopes...)
	if requested != nil {
		available := make(map[Scope]bool, len(record.Scopes))
		for _, scope := range record.Scopes {
			available[scope] = true
		}
		seen := make(map[Scope]bool, len(requested))
		granted = granted[:0]
		for _, scope := range requested {
			if !knownScope(scope) || !available[scope] || seen[scope] {
				return ClientCredential{}, errors.New("requested scope is not allowed by enrollment")
			}
			seen[scope] = true
			granted = append(granted, scope)
		}
		if len(granted) == 0 {
			return ClientCredential{}, errors.New("at least one client scope is required")
		}
	}
	clientIDBytes, err := s.randomBytes(12)
	if err != nil {
		return ClientCredential{}, err
	}
	secret, err := s.randomBytes(32)
	if err != nil {
		return ClientCredential{}, err
	}
	clientID := hex.EncodeToString(clientIDBytes)
	token := "ivoai-client_" + clientID + "_" + base64.RawURLEncoding.EncodeToString(secret)
	now := s.now()
	record.ConsumedAt = now
	// Remove the verifier after successful use. The code cannot be recovered or retried.
	record.CodeHash = ""
	current.Enrollments[id] = record
	current.Clients[clientID] = storedClient{ID: clientID, Name: clientName, TokenHash: hash(token), Scopes: append([]Scope(nil), granted...), CreatedAt: now}
	if err := s.save(current); err != nil {
		return ClientCredential{}, err
	}
	return ClientCredential{ClientID: clientID, Token: token, Scopes: append([]Scope(nil), granted...)}, nil
}

func knownScope(scope Scope) bool {
	switch scope {
	case ScopeContextRead, ScopeMemoryRead, ScopeMemoryWrite, ScopeStatusRead, ScopeDoctorRead, ScopeConnectorRead:
		return true
	default:
		return false
	}
}

func (s *Store) List() ([]Enrollment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockState()
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := s.load()
	if err != nil {
		return nil, err
	}
	list := make([]Enrollment, 0, len(current.Enrollments))
	for _, record := range current.Enrollments {
		list = append(list, record.Enrollment)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list, nil
}

func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockState()
	if err != nil {
		return err
	}
	defer unlock()
	current, err := s.load()
	if err != nil {
		return err
	}
	record, found := current.Enrollments[id]
	if !found {
		return errors.New("enrollment not found")
	}
	if record.RevokedAt.IsZero() {
		record.RevokedAt = s.now()
		record.CodeHash = ""
		current.Enrollments[id] = record
	}
	return s.save(current)
}

func tokenID(token string) (string, bool) {
	const prefix = "ivoai-client_"
	if !strings.HasPrefix(token, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(token, prefix)
	if len(remainder) <= 25 || remainder[24] != '_' {
		return "", false
	}
	id := remainder[:24]
	if _, err := hex.DecodeString(id); err != nil {
		return "", false
	}
	return id, true
}

func (s *Store) Authenticate(token string, required ...Scope) (Principal, error) {
	id, valid := tokenID(token)
	if !valid {
		return Principal{}, errors.New("invalid client credential")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	unlock, err := s.lockStateShared()
	if err != nil {
		return Principal{}, err
	}
	defer unlock()
	current, err := s.load()
	if err != nil {
		return Principal{}, err
	}
	client, found := current.Clients[id]
	if !found || !client.RevokedAt.IsZero() || subtle.ConstantTimeCompare([]byte(client.TokenHash), []byte(hash(token))) != 1 {
		return Principal{}, errors.New("invalid client credential")
	}
	available := make(map[Scope]bool, len(client.Scopes))
	for _, scope := range client.Scopes {
		available[scope] = true
	}
	for _, scope := range required {
		if !available[scope] {
			return Principal{}, errors.New("client credential lacks required scope")
		}
	}
	return Principal{ClientID: client.ID, Name: client.Name, Scopes: append([]Scope(nil), client.Scopes...)}, nil
}

func (s *Store) RevokeClient(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	unlock, err := s.lockState()
	if err != nil {
		return err
	}
	defer unlock()
	current, err := s.load()
	if err != nil {
		return err
	}
	client, found := current.Clients[id]
	if !found {
		return errors.New("client not found")
	}
	client.RevokedAt = s.now()
	client.TokenHash = ""
	current.Clients[id] = client
	return s.save(current)
}
