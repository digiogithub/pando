package natives

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"
)

func testFS(content string) fstest.MapFS {
	return fstest.MapFS{
		"linux-amd64/.keep":            {Data: []byte("placeholder")},
		"linux-amd64/libghostty-vt.so": {Data: []byte(content)},
	}
}

func wantPath(root, content string) string {
	sum := sha256.Sum256([]byte(content))
	return filepath.Join(root, hex.EncodeToString(sum[:])[:16], "libghostty-vt.so")
}

func TestInstallWritesContentAddressed(t *testing.T) {
	root := t.TempDir()
	p, err := install(testFS("lib-v1"), "linux-amd64/libghostty-vt.so", root)
	if err != nil {
		t.Fatal(err)
	}
	if want := wantPath(root, "lib-v1"); p != want {
		t.Fatalf("path = %q, want %q", p, want)
	}
	b, err := os.ReadFile(p)
	if err != nil || string(b) != "lib-v1" {
		t.Fatalf("content = %q, %v", b, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o755 {
			t.Fatalf("mode = %v", fi.Mode().Perm())
		}
	}
	// A new library version lands in its own directory.
	p2, err := install(testFS("lib-v2"), "linux-amd64/libghostty-vt.so", root)
	if err != nil || p2 == p || p2 != wantPath(root, "lib-v2") {
		t.Fatalf("second version: %q, %v", p2, err)
	}
}

func TestInstallIdempotent(t *testing.T) {
	root := t.TempDir()
	fsys := testFS("lib")
	p, err := install(fsys, "linux-amd64/libghostty-vt.so", root)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	p2, err := install(fsys, "linux-amd64/libghostty-vt.so", root)
	if err != nil || p2 != p {
		t.Fatalf("second install: %q, %v", p2, err)
	}
	if fi, _ := os.Stat(p); !fi.ModTime().Equal(old) {
		t.Fatalf("file rewritten although its content matched (mtime %v)", fi.ModTime())
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestInstallRewritesMismatch(t *testing.T) {
	root := t.TempDir()
	fsys := testFS("good")
	p := wantPath(root, "good")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := install(fsys, "linux-amd64/libghostty-vt.so", root)
	if err != nil || got != p {
		t.Fatalf("install: %q, %v", got, err)
	}
	if b, _ := os.ReadFile(p); string(b) != "good" {
		t.Fatalf("content = %q, want rewritten", b)
	}
}

func TestInstallNoEmbeddedLibrary(t *testing.T) {
	root := t.TempDir()
	fsys := fstest.MapFS{"linux-amd64/.keep": {Data: []byte("placeholder")}}
	p, err := install(fsys, "linux-amd64/libghostty-vt.so", root)
	if err != nil || p != "" {
		t.Fatalf("install = %q, %v; want no-op", p, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("wrote %v", entries)
	}
}

func TestInstallRespectsUserEnv(t *testing.T) {
	t.Setenv(EnvGhosttyVT, "/custom/libghostty-vt.so")
	t.Setenv(envOwned, "")
	p, err := Install()
	if err != nil || p != "" {
		t.Fatalf("Install = %q, %v; want no-op", p, err)
	}
	if got := os.Getenv(EnvGhosttyVT); got != "/custom/libghostty-vt.so" {
		t.Fatalf("env changed to %q", got)
	}
}

func TestInstallFromBinary(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HOME", cache)
	t.Setenv("LocalAppData", cache)
	// An inherited value written by another pando is not the user's choice.
	t.Setenv(EnvGhosttyVT, "/other/pando/lib")
	t.Setenv(envOwned, "/other/pando/lib")
	p, err := Install()
	if err != nil {
		t.Fatal(err)
	}
	if !Embedded() {
		if p != "" || os.Getenv(EnvGhosttyVT) != "/other/pando/lib" {
			t.Fatalf("no embedded library: Install = %q, env %q", p, os.Getenv(EnvGhosttyVT))
		}
		t.Skip("binary embeds no libghostty-vt (run make natives first)")
	}
	if p == "" || os.Getenv(EnvGhosttyVT) != p || os.Getenv(envOwned) != p {
		t.Fatalf("Install = %q, env %q", p, os.Getenv(EnvGhosttyVT))
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
