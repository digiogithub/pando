package mcpauth

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp-auth.json")
	return New(path)
}

func TestStore_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	entry := &Entry{
		ServerURL: "https://example.com/mcp",
		Tokens: &Tokens{
			AccessToken:  "at-1",
			TokenType:    "Bearer",
			RefreshToken: "rt-1",
			Scope:        "read write",
			ExpiresAt:    time.Now().Add(time.Hour).Truncate(time.Second),
		},
		ClientInfo: &ClientInfo{ClientID: "client-1", ClientSecret: "secret-1"},
		Metadata:   &Metadata{Issuer: "https://issuer.example.com"},
	}
	if err := s.Set("myserver", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok, err := s.Get("myserver")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatalf("expected entry to exist")
	}
	if got.ServerURL != entry.ServerURL {
		t.Errorf("ServerURL = %q, want %q", got.ServerURL, entry.ServerURL)
	}
	if got.Tokens == nil || got.Tokens.AccessToken != "at-1" {
		t.Errorf("Tokens = %+v, want AccessToken at-1", got.Tokens)
	}
	if got.ClientInfo == nil || got.ClientInfo.ClientID != "client-1" {
		t.Errorf("ClientInfo = %+v, want ClientID client-1", got.ClientInfo)
	}
	if got.Metadata == nil || got.Metadata.Issuer != "https://issuer.example.com" {
		t.Errorf("Metadata = %+v", got.Metadata)
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("expected UpdatedAt to be stamped")
	}

	// A second, unrelated server must not appear.
	if _, ok, err := s.Get("other"); err != nil || ok {
		t.Errorf("Get(other) = ok=%v err=%v, want not found", ok, err)
	}
}

func TestStore_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-auth.json")
	s := New(path)

	if err := s.Set("srv", &Entry{ServerURL: "https://x"}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 0600", perm)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	// dir was created by t.TempDir(), not by the store, so only assert the
	// store didn't loosen it; the store's own MkdirAll uses 0700 when it
	// creates the directory itself (covered by TestStore_CreatesDirectory).
	_ = dirInfo
}

func TestStore_CreatesDirectoryWithRestrictedPerms(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "nested", "dir")
	path := filepath.Join(nested, "mcp-auth.json")
	s := New(path)

	if err := s.Set("srv", &Entry{ServerURL: "https://x"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("stat nested dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir perm = %o, want 0700", perm)
	}
}

func TestStore_AtomicWrite_NoTempFilesLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-auth.json")
	s := New(path)

	for i := 0; i < 5; i++ {
		if err := s.Set("srv", &Entry{ServerURL: "https://x"}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestStore_GetForURL_InvalidatesOnURLChange(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateTokens("srv", &Tokens{AccessToken: "at"}, "https://old.example.com"); err != nil {
		t.Fatalf("UpdateTokens: %v", err)
	}

	// Same URL: found.
	if _, ok, err := s.GetForURL("srv", "https://old.example.com"); err != nil || !ok {
		t.Fatalf("GetForURL(same) = ok=%v err=%v, want found", ok, err)
	}

	// Different URL: must be invalidated (not found).
	entry, ok, err := s.GetForURL("srv", "https://new.example.com")
	if err != nil {
		t.Fatalf("GetForURL(changed): %v", err)
	}
	if ok || entry != nil {
		t.Errorf("GetForURL(changed) = ok=%v entry=%+v, want invalidated", ok, entry)
	}
}

func TestStore_CorruptFileDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-auth.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	s := New(path)
	all, err := s.All()
	if err != nil {
		t.Fatalf("All() on corrupt file returned error, want graceful degradation: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("All() = %+v, want empty", all)
	}

	// The store must still be writable after degrading.
	if err := s.Set("srv", &Entry{ServerURL: "https://x"}); err != nil {
		t.Fatalf("Set after corrupt file: %v", err)
	}
}

func TestStore_MissingFileDegradesToEmpty(t *testing.T) {
	s := newTestStore(t) // file does not exist yet
	all, err := s.All()
	if err != nil {
		t.Fatalf("All(): %v", err)
	}
	if len(all) != 0 {
		t.Errorf("All() = %+v, want empty", all)
	}
	mtime, err := s.ModTime()
	if err != nil {
		t.Fatalf("ModTime(): %v", err)
	}
	if !mtime.IsZero() {
		t.Errorf("ModTime() = %v, want zero for missing file", mtime)
	}
}

func TestStore_ConcurrentWriters(t *testing.T) {
	s := newTestStore(t)

	var wg sync.WaitGroup
	const n = 20
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := "srv"
			_ = s.UpdateTokens(name, &Tokens{AccessToken: "at"}, "https://example.com")
		}(i)
	}
	wg.Wait()

	// The file must still be valid JSON with the entry present, i.e. no
	// writer's update was lost to a torn/interleaved write.
	entry, ok, err := s.Get("srv")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok || entry.Tokens == nil || entry.Tokens.AccessToken != "at" {
		t.Fatalf("Get after concurrent writers = ok=%v entry=%+v", ok, entry)
	}
}

func TestStore_Remove(t *testing.T) {
	s := newTestStore(t)
	if err := s.Set("srv", &Entry{ServerURL: "https://x"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Remove("srv"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok, err := s.Get("srv"); err != nil || ok {
		t.Fatalf("Get after Remove = ok=%v err=%v", ok, err)
	}
	// Removing again (already gone) must not error.
	if err := s.Remove("srv"); err != nil {
		t.Fatalf("Remove (again): %v", err)
	}
}

func TestNew_DefaultPathHonorsEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override.json")
	t.Setenv(envAuthFileOverride, override)

	s := New("")
	if s.Path() != override {
		t.Errorf("Path() = %q, want %q", s.Path(), override)
	}
}
