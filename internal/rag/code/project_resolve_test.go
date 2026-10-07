package code

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveProjectID(t *testing.T) {
	db := setupMigratedDB(t)
	idx := NewCodeIndexer(db, nil, 1)
	ctx := context.Background()
	now := time.Now().UTC()

	base := t.TempDir()
	ivoox := filepath.Join(base, "app_ios_ivooxnew")
	telethon := filepath.Join(base, "telethon_downloader")
	for _, dir := range []string{ivoox, telethon} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	insertTestCodeProject(t, db, "app_ios_ivooxnew", ivoox, now)

	resolve := func(requested, root string) string {
		t.Helper()
		got, err := idx.ResolveProjectID(ctx, requested, root)
		if err != nil {
			t.Fatalf("ResolveProjectID(%q, %q) error = %v", requested, root, err)
		}
		return got
	}

	// Same directory requested under another id (sanitized full path) reuses
	// the existing id, also through a non-clean spelling of the path.
	if got := resolve("Users_x_app_ios_ivooxnew", ivoox+"/./"); got != "app_ios_ivooxnew" {
		t.Errorf("same path, other id = %q, want app_ios_ivooxnew", got)
	}

	// Through a symlink as well.
	link := filepath.Join(base, "link")
	if err := os.Symlink(ivoox, link); err == nil {
		if got := resolve("link", link); got != "app_ios_ivooxnew" {
			t.Errorf("symlinked path = %q, want app_ios_ivooxnew", got)
		}
	}

	// A free id for a new directory is kept.
	if got := resolve("telethon_downloader", telethon); got != "telethon_downloader" {
		t.Errorf("free id = %q, want telethon_downloader", got)
	}

	// An id owned by another existing directory is never moved: the new
	// directory gets a suffixed id.
	got := resolve("app_ios_ivooxnew", telethon)
	if got == "app_ios_ivooxnew" || !strings.HasPrefix(got, "app_ios_ivooxnew_") {
		t.Errorf("taken id = %q, want app_ios_ivooxnew_<hash>", got)
	}
	if again := resolve("app_ios_ivooxnew", telethon); again != got {
		t.Errorf("suffixed id not stable: %q then %q", got, again)
	}

	// An id whose directory is gone (moved project) can be reused.
	gone := filepath.Join(base, "gone")
	insertTestCodeProject(t, db, "moved", gone, now)
	if got := resolve("moved", telethon); got != "moved" {
		t.Errorf("id of missing dir = %q, want moved", got)
	}
}
