package catalog

import (
	"testing"
	"time"
)

func TestReadLockNotExist(t *testing.T) {
	dir := t.TempDir()
	lock, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock on empty dir: %v", err)
	}
	if lock.Version != "1" {
		t.Errorf("expected version 1, got %q", lock.Version)
	}
	if len(lock.Skills) != 0 {
		t.Errorf("expected empty skills map, got %v", lock.Skills)
	}
}

func TestWriteAndReadLock(t *testing.T) {
	dir := t.TempDir()
	lock := &CatalogLock{
		Version: "1",
		Skills:  make(map[string]LockEntry),
	}
	lock.Skills["git-workflow"] = LockEntry{
		Name:        "git-workflow",
		Source:      "owner/repo",
		SkillID:     "git-workflow",
		Scope:       "global",
		InstalledAt: time.Now().UTC().Truncate(time.Second),
		Checksum:    ChecksumContent("content"),
	}

	if err := WriteLock(dir, lock); err != nil {
		t.Fatalf("WriteLock: %v", err)
	}

	got, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}

	entry, ok := got.Skills["git-workflow"]
	if !ok {
		t.Fatal("expected git-workflow entry in lock")
	}
	if entry.Source != "owner/repo" {
		t.Errorf("source: got %q, want %q", entry.Source, "owner/repo")
	}
}

func TestAddLockEntry(t *testing.T) {
	dir := t.TempDir()
	entry := LockEntry{
		Name:        "my-skill",
		Source:      "foo/bar",
		SkillID:     "my-skill",
		Scope:       "project",
		InstalledAt: time.Now().UTC(),
		Checksum:    ChecksumContent("# My Skill"),
	}

	if err := AddLockEntry(dir, entry); err != nil {
		t.Fatalf("AddLockEntry: %v", err)
	}

	lock, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if _, ok := lock.Skills["my-skill"]; !ok {
		t.Error("entry not found after AddLockEntry")
	}
}

func TestRemoveLockEntry(t *testing.T) {
	dir := t.TempDir()
	entry := LockEntry{
		Name:        "rm-skill",
		Source:      "a/b",
		SkillID:     "rm-skill",
		Scope:       "global",
		InstalledAt: time.Now().UTC(),
		Checksum:    ChecksumContent("data"),
	}

	if err := AddLockEntry(dir, entry); err != nil {
		t.Fatalf("AddLockEntry: %v", err)
	}
	if err := RemoveLockEntry(dir, "rm-skill"); err != nil {
		t.Fatalf("RemoveLockEntry: %v", err)
	}

	lock, err := ReadLock(dir)
	if err != nil {
		t.Fatalf("ReadLock: %v", err)
	}
	if _, ok := lock.Skills["rm-skill"]; ok {
		t.Error("entry still present after RemoveLockEntry")
	}
}

func TestChecksumContent(t *testing.T) {
	s1 := ChecksumContent("hello")
	s2 := ChecksumContent("hello")
	if s1 != s2 {
		t.Error("same content should produce same checksum")
	}
	if ChecksumContent("hello") == ChecksumContent("world") {
		t.Error("different content should produce different checksums")
	}
	// SHA256 of "hello" is well-known
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if s1 != want {
		t.Errorf("checksum of 'hello': got %q, want %q", s1, want)
	}
}

func TestRemoveLockEntryNotExist(t *testing.T) {
	dir := t.TempDir()
	// Should be a no-op
	if err := RemoveLockEntry(dir, "nonexistent"); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}
