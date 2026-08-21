// Package webauth implements the OAuth 2.1 authorization server used by remote
// web MCP clients. Only hashes of activation codes and bearer tokens are kept.
package webauth

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

const (
	ScopeContextRead  = "context:read"
	ScopeMemoryRead   = "memory:read"
	ScopeMemoryWrite  = "memory:write"
	ScopeMemoryDelete = "memory:delete"
)

var DefaultScopes = []string{ScopeContextRead, ScopeMemoryRead, ScopeMemoryWrite, ScopeMemoryDelete}

type Activation struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ConsumedAt time.Time `json:"consumed_at,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitempty"`
	Scopes     []string  `json:"scopes"`
}
type CreatedActivation struct {
	Activation
	Code string `json:"code"`
}
type GrantView struct {
	ID        string
	Status    string
	ExpiresAt time.Time
	Scopes    []string
}
type Client struct {
	ID           string    `json:"client_id"`
	Name         string    `json:"client_name"`
	RedirectURIs []string  `json:"redirect_uris"`
	CreatedAt    time.Time `json:"created_at"`
}
type Principal struct {
	ClientID string
	Scopes   []string
}
type record struct {
	Activation
	CodeHash string `json:"code_hash"`
}
type authCode struct {
	Hash, ClientID, RedirectURI, Challenge string
	GrantID                                string
	Resource                               string
	Scopes                                 []string
	ExpiresAt                              time.Time
}
type tokenRecord struct {
	Hash, ClientID string
	GrantID        string
	Resource       string
	Scopes         []string
	ExpiresAt      time.Time
	RevokedAt      time.Time
}
type authorizationRequest struct {
	Hash        string    `json:"hash"`
	ClientID    string    `json:"client_id"`
	RedirectURI string    `json:"redirect_uri"`
	Challenge   string    `json:"challenge"`
	Resource    string    `json:"resource"`
	State       string    `json:"state"`
	Scopes      []string  `json:"scopes"`
	ExpiresAt   time.Time `json:"expires_at"`
}
type state struct {
	Activations map[string]record               `json:"activations"`
	Clients     map[string]Client               `json:"clients"`
	Codes       map[string]authCode             `json:"authorization_codes"`
	Access      map[string]tokenRecord          `json:"access_tokens"`
	Refresh     map[string]tokenRecord          `json:"refresh_tokens"`
	Requests    map[string]authorizationRequest `json:"authorization_requests"`
}

type Store struct {
	Path   string
	Clock  func() time.Time
	Random io.Reader
	mu     sync.Mutex
}

func NewStore(path string) *Store { return &Store{Path: path, Clock: time.Now, Random: rand.Reader} }
func (s *Store) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}
func (s *Store) random(n int) ([]byte, error) {
	b := make([]byte, n)
	r := s.Random
	if r == nil {
		r = rand.Reader
	}
	_, err := io.ReadFull(r, b)
	return b, err
}
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func idOf(prefix, value string, bytes int) (string, bool) {
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(value, prefix)
	if len(rest) <= bytes*2 || rest[bytes*2] != '_' {
		return "", false
	}
	id := rest[:bytes*2]
	_, err := hex.DecodeString(id)
	return id, err == nil
}
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
func validScope(s string) bool { return contains(DefaultScopes, s) }
func normalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	seen := map[string]bool{}
	out := []string{}
	for _, v := range scopes {
		v = strings.TrimSpace(v)
		if !validScope(v) || seen[v] {
			return nil, fmt.Errorf("invalid or duplicate scope %q", v)
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) with(exclusive bool, fn func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.Path)
	if info, err := os.Lstat(dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing symlink web OAuth state directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}
	lockPath := s.Path + ".lock"
	if info, err := os.Lstat(lockPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing symlink web OAuth lock")
	}
	fd, err := unix.Open(lockPath, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	if lock == nil {
		_ = unix.Close(fd)
		return errors.New("open web OAuth lock")
	}
	if info, err := lock.Stat(); err != nil || !info.Mode().IsRegular() {
		lock.Close()
		return errors.New("web OAuth lock must be a regular file")
	}
	if err := lock.Chmod(0600); err != nil {
		lock.Close()
		return err
	}
	defer lock.Close()
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	if err = syscall.Flock(int(lock.Fd()), mode); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	st := state{Activations: map[string]record{}, Clients: map[string]Client{}, Codes: map[string]authCode{}, Access: map[string]tokenRecord{}, Refresh: map[string]tokenRecord{}, Requests: map[string]authorizationRequest{}}
	if info, e := os.Lstat(s.Path); e == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("web OAuth state must be a private regular file")
		}
		fd, e := unix.Open(s.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if e != nil {
			return e
		}
		f := os.NewFile(uintptr(fd), s.Path)
		after, statErr := f.Stat()
		if statErr != nil || !os.SameFile(info, after) || !after.Mode().IsRegular() {
			f.Close()
			return errors.New("web OAuth state changed while opening")
		}
		limited := &io.LimitedReader{R: f, N: (16 << 20) + 1}
		decoder := json.NewDecoder(limited)
		e = decoder.Decode(&st)
		if e == nil {
			var trailing any
			if trailingErr := decoder.Decode(&trailing); trailingErr != io.EOF {
				e = errors.New("web OAuth state contains trailing data")
			}
		}
		if limited.N <= 0 {
			e = errors.New("web OAuth state exceeds size limit")
		}
		f.Close()
		if e != nil {
			return e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if st.Activations == nil {
		st.Activations = map[string]record{}
	}
	if st.Clients == nil {
		st.Clients = map[string]Client{}
	}
	if st.Codes == nil {
		st.Codes = map[string]authCode{}
	}
	if st.Access == nil {
		st.Access = map[string]tokenRecord{}
	}
	if st.Refresh == nil {
		st.Refresh = map[string]tokenRecord{}
	}
	if st.Requests == nil {
		st.Requests = map[string]authorizationRequest{}
	}
	if exclusive {
		pruneState(&st, s.now())
	}
	if err := fn(&st); err != nil {
		return err
	}
	if !exclusive {
		return nil
	}
	tmp, e := os.CreateTemp(dir, ".oauth-*")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	if e = tmp.Chmod(0600); e == nil {
		e = json.NewEncoder(tmp).Encode(st)
	}
	if e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if info, err := os.Lstat(s.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace symlink web OAuth state")
	}
	if err := os.Rename(name, s.Path); err != nil {
		return err
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr = directory.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func pruneState(current *state, now time.Time) {
	for key, request := range current.Requests {
		if !now.Before(request.ExpiresAt) {
			delete(current.Requests, key)
		}
	}
	for key, code := range current.Codes {
		if !now.Before(code.ExpiresAt) {
			delete(current.Codes, key)
		}
	}
	for key, token := range current.Access {
		if !now.Before(token.ExpiresAt) {
			delete(current.Access, key)
		}
	}
	for key, token := range current.Refresh {
		if !now.Before(token.ExpiresAt) {
			delete(current.Refresh, key)
		}
	}
	for key, activation := range current.Activations {
		remove := !activation.ConsumedAt.IsZero() && !now.Before(activation.ConsumedAt.Add(30*24*time.Hour))
		remove = remove || (!activation.RevokedAt.IsZero() && !now.Before(activation.RevokedAt.Add(30*24*time.Hour)))
		remove = remove || (activation.ConsumedAt.IsZero() && activation.RevokedAt.IsZero() && !now.Before(activation.ExpiresAt.Add(7*24*time.Hour)))
		if remove {
			delete(current.Activations, key)
		}
	}
	// DCR clients carry no secret, but retaining abandoned registrations forever
	// would let unauthenticated traffic exhaust the bounded registry. A client is
	// eligible for cleanup only after 90 days and when no live OAuth object refers
	// to it.
	for key, client := range current.Clients {
		if now.Before(client.CreatedAt.Add(90*24*time.Hour)) || clientReferenced(current, key) {
			continue
		}
		delete(current.Clients, key)
	}
}

func clientReferenced(current *state, clientID string) bool {
	for _, request := range current.Requests {
		if request.ClientID == clientID {
			return true
		}
	}
	for _, code := range current.Codes {
		if code.ClientID == clientID {
			return true
		}
	}
	for _, token := range current.Access {
		if token.ClientID == clientID {
			return true
		}
	}
	for _, token := range current.Refresh {
		if token.ClientID == clientID {
			return true
		}
	}
	return false
}

func (s *Store) CreateActivation(ttl time.Duration, scopes []string) (CreatedActivation, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		return CreatedActivation{}, errors.New("web access TTL must be between zero and 24 hours")
	}
	scopes, err := normalizeScopes(scopes)
	if err != nil {
		return CreatedActivation{}, err
	}
	idb, err := s.random(8)
	if err != nil {
		return CreatedActivation{}, err
	}
	secret, err := s.random(32)
	if err != nil {
		return CreatedActivation{}, err
	}
	id := hex.EncodeToString(idb)
	code := "ivoai-web_" + id + "_" + base64.RawURLEncoding.EncodeToString(secret)
	a := Activation{ID: id, CreatedAt: s.now(), ExpiresAt: s.now().Add(ttl), Scopes: scopes}
	err = s.with(true, func(st *state) error {
		if len(st.Activations) >= 4096 {
			return errors.New("web access activation capacity reached")
		}
		st.Activations[id] = record{Activation: a, CodeHash: digest(code)}
		return nil
	})
	return CreatedActivation{Activation: a, Code: code}, err
}
func (s *Store) ListActivations() ([]Activation, error) {
	var out []Activation
	err := s.with(false, func(st *state) error {
		for _, r := range st.Activations {
			out = append(out, r.Activation)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, err
}
func (s *Store) ListGrants() ([]GrantView, error) {
	items, err := s.ListActivations()
	if err != nil {
		return nil, err
	}
	now := s.now()
	views := make([]GrantView, 0, len(items))
	for _, item := range items {
		status := "pending"
		expires := item.ExpiresAt
		switch {
		case !item.RevokedAt.IsZero():
			status = "revoked"
		case !item.ConsumedAt.IsZero():
			status = "authorized"
			expires = item.ConsumedAt.Add(30 * 24 * time.Hour)
			if !now.Before(expires) {
				status = "expired"
			}
		case !now.Before(expires):
			status = "expired"
		}
		views = append(views, GrantView{ID: item.ID, Status: status, ExpiresAt: expires, Scopes: append([]string(nil), item.Scopes...)})
	}
	return views, nil
}
func (s *Store) RevokeActivation(id string) error {
	return s.with(true, func(st *state) error {
		r, ok := st.Activations[id]
		if !ok {
			return errors.New("web access activation not found")
		}
		r.RevokedAt = s.now()
		r.CodeHash = ""
		st.Activations[id] = r
		for key, code := range st.Codes {
			if code.GrantID == id {
				delete(st.Codes, key)
			}
		}
		for key, token := range st.Access {
			if token.GrantID == id {
				delete(st.Access, key)
			}
		}
		for key, token := range st.Refresh {
			if token.GrantID == id {
				delete(st.Refresh, key)
			}
		}
		return nil
	})
}

func (s *Store) RegisterClient(name string, redirects []string) (Client, error) {
	if strings.TrimSpace(name) == "" || len(name) > 128 || len(redirects) == 0 || len(redirects) > 8 {
		return Client{}, errors.New("valid client name and redirect URIs are required")
	}
	for _, u := range redirects {
		if err := ValidateRedirectURI(u); err != nil {
			return Client{}, err
		}
	}
	b, err := s.random(16)
	if err != nil {
		return Client{}, err
	}
	c := Client{ID: hex.EncodeToString(b), Name: name, RedirectURIs: append([]string(nil), redirects...), CreatedAt: s.now()}
	err = s.with(true, func(st *state) error {
		if len(st.Clients) >= 4096 {
			return errors.New("OAuth client registry capacity reached")
		}
		st.Clients[c.ID] = c
		return nil
	})
	return c, err
}
func (s *Store) Client(id, redirect string) (Client, error) {
	var c Client
	err := s.with(false, func(st *state) error {
		var ok bool
		c, ok = st.Clients[id]
		if !ok || !contains(c.RedirectURIs, redirect) {
			return errors.New("unknown OAuth client or redirect URI")
		}
		return nil
	})
	return c, err
}

func (s *Store) BeginAuthorization(clientID, redirect, challenge, oauthState, resource string, scopes []string) (string, error) {
	if resource == "" {
		return "", errors.New("OAuth resource is required")
	}
	if len(challenge) < 43 || len(challenge) > 128 {
		return "", errors.New("PKCE S256 challenge is required")
	}
	scopes, err := normalizeScopes(scopes)
	if err != nil {
		return "", err
	}
	if _, err = s.Client(clientID, redirect); err != nil {
		return "", err
	}
	b, err := s.random(32)
	if err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(b)
	err = s.with(true, func(st *state) error {
		if len(st.Requests) >= 1024 {
			return errors.New("authorization request capacity reached")
		}
		st.Requests[digest(nonce)] = authorizationRequest{Hash: digest(nonce), ClientID: clientID, RedirectURI: redirect, Challenge: challenge, State: oauthState, Resource: resource, Scopes: scopes, ExpiresAt: s.now().Add(10 * time.Minute)}
		return nil
	})
	return nonce, err
}

func (s *Store) AuthorizeRequest(activation, nonce string) (string, string, string, error) {
	var code, redirect, oauthState string
	err := s.with(true, func(current *state) error {
		key := digest(nonce)
		request, ok := current.Requests[key]
		delete(current.Requests, key)
		if !ok || subtle.ConstantTimeCompare([]byte(request.Hash), []byte(key)) != 1 || !s.now().Before(request.ExpiresAt) {
			return errors.New("invalid authorization request")
		}
		grantID, valid := idOf("ivoai-web_", activation, 8)
		if !valid {
			return errors.New("invalid or expired activation code")
		}
		activationRecord, ok := current.Activations[grantID]
		if !ok || subtle.ConstantTimeCompare([]byte(activationRecord.CodeHash), []byte(digest(activation))) != 1 || !activationRecord.ConsumedAt.IsZero() || !activationRecord.RevokedAt.IsZero() || !s.now().Before(activationRecord.ExpiresAt) {
			return errors.New("invalid or expired activation code")
		}
		for _, requestedScope := range request.Scopes {
			if !contains(activationRecord.Scopes, requestedScope) {
				return errors.New("requested scope is not approved")
			}
		}
		secret, err := s.random(32)
		if err != nil {
			return err
		}
		code = "ivoai-code_" + base64.RawURLEncoding.EncodeToString(secret)
		redirect = request.RedirectURI
		oauthState = request.State
		activationRecord.ConsumedAt = s.now()
		activationRecord.CodeHash = ""
		current.Activations[grantID] = activationRecord
		current.Codes[digest(code)] = authCode{Hash: digest(code), ClientID: request.ClientID, RedirectURI: request.RedirectURI, Challenge: request.Challenge, GrantID: grantID, Resource: request.Resource, Scopes: request.Scopes, ExpiresAt: s.now().Add(5 * time.Minute)}
		return nil
	})
	return code, redirect, oauthState, err
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (s *Store) issue(st *state, clientID, grantID, resource string, scopes []string) (Tokens, error) {
	ab, e := s.random(32)
	if e != nil {
		return Tokens{}, e
	}
	rb, e := s.random(32)
	if e != nil {
		return Tokens{}, e
	}
	access := "ivoai-oauth_" + base64.RawURLEncoding.EncodeToString(ab)
	refresh := "ivoai-refresh_" + base64.RawURLEncoding.EncodeToString(rb)
	now := s.now()
	st.Access[digest(access)] = tokenRecord{Hash: digest(access), ClientID: clientID, GrantID: grantID, Resource: resource, Scopes: scopes, ExpiresAt: now.Add(time.Hour)}
	st.Refresh[digest(refresh)] = tokenRecord{Hash: digest(refresh), ClientID: clientID, GrantID: grantID, Resource: resource, Scopes: scopes, ExpiresAt: now.Add(30 * 24 * time.Hour)}
	return Tokens{AccessToken: access, TokenType: "Bearer", ExpiresIn: 3600, RefreshToken: refresh, Scope: strings.Join(scopes, " ")}, nil
}
func PKCEChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
func (s *Store) ExchangeCode(code, clientID, redirect, verifier, resource string) (Tokens, error) {
	var out Tokens
	err := s.with(true, func(st *state) error {
		k := digest(code)
		c, ok := st.Codes[k]
		delete(st.Codes, k)
		if !ok || c.ClientID != clientID || c.RedirectURI != redirect || c.Resource != resource || !s.now().Before(c.ExpiresAt) || subtle.ConstantTimeCompare([]byte(c.Challenge), []byte(PKCEChallenge(verifier))) != 1 {
			return errors.New("invalid authorization code")
		}
		var e error
		out, e = s.issue(st, clientID, c.GrantID, c.Resource, c.Scopes)
		return e
	})
	return out, err
}
func (s *Store) Refresh(token, clientID, resource string) (Tokens, error) {
	var out Tokens
	err := s.with(true, func(st *state) error {
		k := digest(token)
		r, ok := st.Refresh[k]
		delete(st.Refresh, k)
		if !ok || r.ClientID != clientID || r.Resource != resource || !r.RevokedAt.IsZero() || !s.now().Before(r.ExpiresAt) {
			return errors.New("invalid refresh token")
		}
		grant, ok := st.Activations[r.GrantID]
		if !ok || !grant.RevokedAt.IsZero() {
			return errors.New("invalid refresh token")
		}
		var e error
		out, e = s.issue(st, clientID, r.GrantID, r.Resource, r.Scopes)
		return e
	})
	return out, err
}
func (s *Store) Authenticate(token, resource string, required ...string) (Principal, error) {
	var p Principal
	err := s.with(false, func(st *state) error {
		r, ok := st.Access[digest(token)]
		if !ok || r.Resource != resource || !r.RevokedAt.IsZero() || !s.now().Before(r.ExpiresAt) || subtle.ConstantTimeCompare([]byte(r.Hash), []byte(digest(token))) != 1 {
			return errors.New("invalid access token")
		}
		for _, v := range required {
			if !contains(r.Scopes, v) {
				return errors.New("insufficient scope")
			}
		}
		p = Principal{ClientID: r.ClientID, Scopes: append([]string(nil), r.Scopes...)}
		return nil
	})
	return p, err
}
func (s *Store) RevokeToken(token string) error {
	return s.with(true, func(st *state) error { k := digest(token); delete(st.Access, k); delete(st.Refresh, k); return nil })
}
