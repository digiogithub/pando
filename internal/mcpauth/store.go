// Package mcpauth implements Phase 2 of the MCP client authentication plan:
// persistent OAuth 2.1 credential storage and a process-wide manager that
// builds mcp-go transport.OAuthConfig values from Pando's declarative
// config.MCPAuth blocks. It does not drive the interactive browser/callback
// flow (that is Phase 3); it only stores tokens and dynamically-registered
// client credentials so that Phase 3 (and refresh) can reuse them.
package mcpauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// defaultFileName is the name of the persistent credential file inside
// config.GlobalConfigDir().
const defaultFileName = "mcp-auth.json"

// envAuthFileOverride lets tests and headless setups redirect the store to a
// different path without touching the real user config directory.
const envAuthFileOverride = "PANDO_MCP_AUTH_FILE"

// Tokens is the persisted form of an OAuth token set for one MCP server.
type Tokens struct {
	AccessToken  string    `json:"accessToken,omitempty"`
	TokenType    string    `json:"tokenType,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitzero"`
}

// ClientInfo is the persisted form of a dynamically-registered (RFC 7591)
// OAuth client. mcp-go performs dynamic client registration but does not
// persist its result across process restarts; this fills that gap.
type ClientInfo struct {
	ClientID        string `json:"clientID,omitempty"`
	ClientSecret    string `json:"clientSecret,omitempty"`
	IssuedAt        int64  `json:"issuedAt,omitempty"`
	SecretExpiresAt int64  `json:"secretExpiresAt,omitempty"`
}

// Metadata caches authorization-server metadata discovered for a server, so
// later phases (step-up auth, iss validation, disk-watch invalidation) don't
// need to re-discover it.
//
// AuthorizationResponseIssParameterSupported is populated in Phase 4 from a
// raw fetch of the AS metadata document (see issuer.go): mcp-go v0.57.0's
// client/transport.AuthServerMetadata struct (RFC 8414) does not expose the
// RFC 9207 §3 authorization_response_iss_parameter_supported flag, so
// mcpauth decodes that one field itself rather than relying on mcp-go's
// parsed struct.
type Metadata struct {
	Issuer                                     string `json:"issuer,omitempty"`
	AuthorizationResponseIssParameterSupported bool   `json:"authorizationResponseIssParameterSupported,omitempty"`
}

// Entry is the persisted OAuth state for a single named MCP server.
type Entry struct {
	// ServerURL is the server's base URL at the time these credentials were
	// stored. If a later config points the same server name at a different
	// URL, the credentials are treated as invalid (see Store.GetForURL) —
	// mirroring opencode's getForUrl invalidation-on-change behavior.
	ServerURL  string      `json:"serverURL,omitempty"`
	Tokens     *Tokens     `json:"tokens,omitempty"`
	ClientInfo *ClientInfo `json:"clientInfo,omitempty"`
	Metadata   *Metadata   `json:"metadata,omitempty"`
	// RequestedScopes is the accumulated set of OAuth scopes ever requested
	// for this server, across the initial login and any subsequent 403
	// insufficient_scope step-up re-authorizations (see stepup.go). The MCP
	// authorization spec requires clients to accumulate scopes on step-up
	// rather than replace them, so a later, narrower request never drops a
	// permission the user already granted.
	RequestedScopes []string  `json:"requestedScopes,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
}

// clone returns a deep copy of e, or nil if e is nil.
func (e *Entry) clone() *Entry {
	if e == nil {
		return nil
	}
	out := *e
	if e.Tokens != nil {
		tok := *e.Tokens
		out.Tokens = &tok
	}
	if e.ClientInfo != nil {
		ci := *e.ClientInfo
		out.ClientInfo = &ci
	}
	if e.Metadata != nil {
		md := *e.Metadata
		out.Metadata = &md
	}
	if e.RequestedScopes != nil {
		out.RequestedScopes = append([]string(nil), e.RequestedScopes...)
	}
	return &out
}

// fileFormat is the on-disk JSON document.
type fileFormat struct {
	Version int              `json:"version"`
	Servers map[string]Entry `json:"servers"`
}

// Store is a persistent, file-backed collection of per-server OAuth Entry
// values. It is safe for concurrent use from multiple goroutines in this
// process (via an internal mutex) and from multiple Pando processes (via a
// best-effort cross-process file lock around each read-modify-write).
//
// Store degrades gracefully: a missing or corrupt file is treated as an
// empty store rather than a hard error, since losing cached OAuth state is
// recoverable (re-authorize) while crashing on it is not acceptable.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns a Store backed by path. An empty path resolves to the default
// location: config.GlobalConfigDir()/mcp-auth.json, unless the
// PANDO_MCP_AUTH_FILE environment variable is set, in which case that value
// wins (used by tests and headless setups that redirect the global config
// dir elsewhere).
func New(path string) *Store {
	if path == "" {
		path = resolveDefaultPath()
	}
	return &Store{path: path}
}

func resolveDefaultPath() string {
	if override := os.Getenv(envAuthFileOverride); override != "" {
		return override
	}
	dir := config.GlobalConfigDir()
	if dir == "" {
		// Fall back to the current directory rather than an empty path,
		// which would resolve to the process cwd's root and could collide
		// across unrelated invocations.
		dir = "."
	}
	return filepath.Join(dir, defaultFileName)
}

// defaultStore is the lazily-initialized singleton returned by Default().
var (
	defaultStoreOnce sync.Once
	defaultStore     *Store
)

// DefaultStore returns the process-wide Store using the default path
// resolution rules described in New.
func DefaultStore() *Store {
	defaultStoreOnce.Do(func() {
		defaultStore = New("")
	})
	return defaultStore
}

// Path returns the file path this store reads from and writes to.
func (s *Store) Path() string {
	return s.path
}

// load reads and parses the file, tolerating a missing or corrupt file by
// returning an empty document. Callers must hold s.mu.
func (s *Store) load() (fileFormat, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileFormat{Version: 1, Servers: map[string]Entry{}}, nil
		}
		logging.Warn("mcpauth: failed to read credential store, treating as empty", "path", s.path, "error", err)
		return fileFormat{Version: 1, Servers: map[string]Entry{}}, nil
	}
	if len(data) == 0 {
		return fileFormat{Version: 1, Servers: map[string]Entry{}}, nil
	}
	var doc fileFormat
	if err := json.Unmarshal(data, &doc); err != nil {
		logging.Warn("mcpauth: credential store is corrupt, treating as empty", "path", s.path, "error", err)
		return fileFormat{Version: 1, Servers: map[string]Entry{}}, nil
	}
	if doc.Servers == nil {
		doc.Servers = map[string]Entry{}
	}
	for name, entry := range doc.Servers {
		doc.Servers[name] = decryptEntryValues(name, entry)
	}
	return doc, nil
}

// save writes the document atomically: it writes to a temp file in the same
// directory, chmods it to 0600, then renames it over the target path.
// Callers must hold s.mu.
func (s *Store) save(doc fileFormat) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mcpauth: create store directory: %w", err)
	}

	// Encrypt sensitive values on a copy, never the doc the caller may still
	// hold a reference to. A legacy plaintext file loaded moments ago (see
	// load's decryptEntryValues, a no-op on unprefixed values) is re-encrypted
	// here on its next write, satisfying the "upgraded on write" requirement
	// without a separate migration step.
	toWrite := fileFormat{Version: doc.Version, Servers: make(map[string]Entry, len(doc.Servers))}
	for name, entry := range doc.Servers {
		encrypted, _ := encryptEntryValues(name, entry)
		toWrite.Servers[name] = encrypted
	}

	data, err := json.MarshalIndent(toWrite, "", "  ")
	if err != nil {
		return fmt.Errorf("mcpauth: marshal credential store: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".mcp-auth-*.tmp")
	if err != nil {
		return fmt.Errorf("mcpauth: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("mcpauth: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mcpauth: close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("mcpauth: chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("mcpauth: rename temp file into place: %w", err)
	}
	removeTmp = false
	return nil
}

// withFileLock runs fn while holding both the in-process mutex and a
// best-effort cross-process lock on s.path+".lock".
//
// This repo already has a flock-based cross-process lock in internal/ipc,
// but it is tailored to the IPC lock file's own JSON schema (LockInfo) and
// lives in an internal package not meant for reuse elsewhere. Rather than
// bend that abstraction, mcpauth uses the simpler mechanism the phase spec
// explicitly allows for: an O_CREATE|O_EXCL sentinel file with a short
// retry/timeout loop. This is sufficient here because the critical section
// is a fast read-modify-write of a small JSON file, not a long-lived
// exclusive session lock.
func (s *Store) withFileLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	unlock, err := acquireFileLock(s.path + ".lock")
	if err != nil {
		// Degrade rather than fail hard: worst case is a lost update under
		// heavy cross-process contention, which is acceptable for cached
		// OAuth credentials that can always be re-derived.
		logging.Warn("mcpauth: failed to acquire cross-process lock, proceeding without it", "path", s.path, "error", err)
		return fn()
	}
	defer unlock()
	return fn()
}

const (
	lockRetryInterval = 20 * time.Millisecond
	lockTimeout       = 2 * time.Second
)

// acquireFileLock creates lockPath exclusively, retrying briefly if it
// already exists (another process holds it), and returns a function that
// removes it. Stale locks older than lockTimeout are forcibly cleared so a
// crashed process can never wedge the store forever.
func acquireFileLock(lockPath string) (func(), error) {
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create lock file: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > lockTimeout {
			_ = os.Remove(lockPath) // stale lock from a crashed process
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for lock %s", lockPath)
		}
		time.Sleep(lockRetryInterval)
	}
}

// ModTime returns the last modification time of the store file. It returns
// the zero time (no error) if the file does not yet exist, so callers can
// use it for cheap external-change detection without treating "never
// written" as an error.
func (s *Store) ModTime() (time.Time, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("mcpauth: stat credential store: %w", err)
	}
	return info.ModTime(), nil
}

// All returns a snapshot of every stored entry, keyed by server name.
func (s *Store) All() (map[string]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Entry, len(doc.Servers))
	for k, v := range doc.Servers {
		out[k] = *v.clone()
	}
	return out, nil
}

// Get returns the stored entry for name, if any.
func (s *Store) Get(name string) (*Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc, err := s.load()
	if err != nil {
		return nil, false, err
	}
	entry, ok := doc.Servers[name]
	if !ok {
		return nil, false, nil
	}
	return entry.clone(), true, nil
}

// GetForURL returns the stored entry for name only if its recorded
// ServerURL matches serverURL (or the stored entry has no ServerURL
// recorded yet, e.g. from an older schema version). If the server's
// configured URL has changed since the credentials were stored, the entry
// is treated as not found — this invalidates stale credentials the way
// opencode's getForUrl does, since an OAuth token/registration is scoped to
// a specific resource server.
func (s *Store) GetForURL(name, serverURL string) (*Entry, bool, error) {
	entry, ok, err := s.Get(name)
	if err != nil || !ok {
		return nil, ok, err
	}
	if entry.ServerURL != "" && serverURL != "" && entry.ServerURL != serverURL {
		return nil, false, nil
	}
	return entry, true, nil
}

// Set stores (replacing) the entry for name, stamping UpdatedAt.
func (s *Store) Set(name string, e *Entry) error {
	if e == nil {
		return errors.New("mcpauth: nil entry")
	}
	return s.withFileLock(func() error {
		doc, err := s.load()
		if err != nil {
			return err
		}
		stored := *e.clone()
		stored.UpdatedAt = time.Now().UTC()
		doc.Servers[name] = stored
		return s.save(doc)
	})
}

// UpdateTokens merges t into the stored entry for name (creating one if
// absent), stamping its ServerURL and UpdatedAt.
func (s *Store) UpdateTokens(name string, t *Tokens, serverURL string) error {
	return s.withFileLock(func() error {
		doc, err := s.load()
		if err != nil {
			return err
		}
		entry := doc.Servers[name]
		if serverURL != "" {
			entry.ServerURL = serverURL
		}
		if t != nil {
			cp := *t
			entry.Tokens = &cp
		} else {
			entry.Tokens = nil
		}
		entry.UpdatedAt = time.Now().UTC()
		doc.Servers[name] = entry
		return s.save(doc)
	})
}

// UpdateClientInfo merges ci into the stored entry for name (creating one
// if absent), stamping its ServerURL and UpdatedAt. This is how dynamic
// client registration results are persisted (see manager.go
// PersistClientRegistration), since mcp-go itself does not persist them.
func (s *Store) UpdateClientInfo(name string, ci *ClientInfo, serverURL string) error {
	return s.withFileLock(func() error {
		doc, err := s.load()
		if err != nil {
			return err
		}
		entry := doc.Servers[name]
		if serverURL != "" {
			entry.ServerURL = serverURL
		}
		if ci != nil {
			cp := *ci
			entry.ClientInfo = &cp
		} else {
			entry.ClientInfo = nil
		}
		entry.UpdatedAt = time.Now().UTC()
		doc.Servers[name] = entry
		return s.save(doc)
	})
}

// UpdateMetadata merges md into the stored entry for name (creating one if
// absent), stamping its ServerURL and UpdatedAt. This is how the Phase 4
// issuer-validation flow caches discovered AS metadata (issuer string plus
// the raw authorization_response_iss_parameter_supported flag) so that a
// later callback can validate iss without re-discovering metadata.
func (s *Store) UpdateMetadata(name string, md *Metadata, serverURL string) error {
	return s.withFileLock(func() error {
		doc, err := s.load()
		if err != nil {
			return err
		}
		entry := doc.Servers[name]
		if serverURL != "" {
			entry.ServerURL = serverURL
		}
		if md != nil {
			cp := *md
			entry.Metadata = &cp
		} else {
			entry.Metadata = nil
		}
		entry.UpdatedAt = time.Now().UTC()
		doc.Servers[name] = entry
		return s.save(doc)
	})
}

// UpdateRequestedScopes replaces the stored entry's accumulated
// RequestedScopes for name (creating an entry if absent), stamping its
// ServerURL and UpdatedAt. Callers are expected to have already computed the
// union with any previously requested scopes (see Manager.StepUpScopes)
// before calling this — Store itself just persists whatever slice it is
// given.
func (s *Store) UpdateRequestedScopes(name string, scopes []string, serverURL string) error {
	return s.withFileLock(func() error {
		doc, err := s.load()
		if err != nil {
			return err
		}
		entry := doc.Servers[name]
		if serverURL != "" {
			entry.ServerURL = serverURL
		}
		if len(scopes) == 0 {
			entry.RequestedScopes = nil
		} else {
			entry.RequestedScopes = append([]string(nil), scopes...)
		}
		entry.UpdatedAt = time.Now().UTC()
		doc.Servers[name] = entry
		return s.save(doc)
	})
}

// Remove deletes the stored entry for name, if any. It is not an error to
// remove an entry that does not exist.
func (s *Store) Remove(name string) error {
	return s.withFileLock(func() error {
		doc, err := s.load()
		if err != nil {
			return err
		}
		delete(doc.Servers, name)
		return s.save(doc)
	})
}
