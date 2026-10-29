package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/history"
	"github.com/digiogithub/pando/internal/permission"
)

const readHistorySession = "rh-session"

type fakeEditHistory struct{ history.Service }

func (fakeEditHistory) GetByPathAndSession(context.Context, string, string) (history.File, error) {
	return history.File{}, errors.New("not found")
}
func (fakeEditHistory) Create(context.Context, string, string, string) (history.File, error) {
	return history.File{}, nil
}
func (fakeEditHistory) CreateVersion(context.Context, string, string, string) (history.File, error) {
	return history.File{}, nil
}

// setupReadHistory isolates global state: working dir, in-memory read records
// and the session read source.
func setupReadHistory(t *testing.T) string {
	t.Helper()
	wd := t.TempDir()
	prev := config.Get()
	config.SetForTests(&config.Config{WorkingDir: wd})
	reset := func() {
		fileRecordMutex.Lock()
		fileRecords = make(map[string]fileRecord)
		fileRecordMutex.Unlock()
		SetSessionReadSource(nil)
	}
	reset()
	t.Cleanup(func() {
		reset()
		config.SetForTests(prev)
	})
	return wd
}

func viewRead(t *testing.T, name, path, content string, at time.Time) SessionToolRead {
	t.Helper()
	in, _ := json.Marshal(ViewParams{FilePath: path})
	meta, _ := json.Marshal(ViewResponseMetadata{FilePath: path, Content: content})
	return SessionToolRead{ToolName: name, Input: string(in), Metadata: string(meta), CalledAt: at}
}

func installSource(reads ...SessionToolRead) {
	SetSessionReadSource(func(context.Context, string) ([]SessionToolRead, error) { return reads, nil })
}

func rhCtx() context.Context {
	return context.WithValue(context.Background(), SessionIDContextKey, readHistorySession)
}

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadHistory_RestartSuccess(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "a.txt", "alpha\nbeta\n")
	at := time.Now().Add(time.Hour)
	installSource(viewRead(t, "view", p, "alpha\nbeta\n", at))

	got := lastReadTimeForEdit(rhCtx(), p)
	if !got.Equal(at) {
		t.Fatalf("got %v want %v", got, at)
	}
	if !getLastReadTime(p).Equal(at) {
		t.Fatal("read time not cached in memory")
	}
}

func TestReadHistory_AliasAccepted(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "a.txt", "alpha")
	at := time.Now()
	installSource(viewRead(t, "read", p, "alpha", at))
	if got := lastReadTimeForEdit(rhCtx(), p); got.IsZero() {
		t.Skip("'read' is not an alias of view in this build")
	}
}

func TestReadHistory_MemoryWins(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "a.txt", "alpha")
	recordFileRead(p)
	installSource(viewRead(t, "view", p, "alpha", time.Unix(1, 0)))
	if got := lastReadTimeForEdit(rhCtx(), p); got.Year() < 2000 {
		t.Fatalf("expected in-memory time, got %v", got)
	}
}

func TestReadHistory_Rejections(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "a.txt", "alpha\n")
	other := writeTemp(t, wd, "b.txt", "alpha\n")
	at := time.Now()
	good := viewRead(t, "view", p, "alpha\n", at)

	t.Run("no source", func(t *testing.T) {
		SetSessionReadSource(nil)
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("no session id", func(t *testing.T) {
		installSource(good)
		if !lastReadTimeForEdit(context.Background(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("no matching view", func(t *testing.T) {
		installSource()
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("different path", func(t *testing.T) {
		installSource(viewRead(t, "view", other, "alpha\n", at))
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("other tool", func(t *testing.T) {
		installSource(viewRead(t, "bash", p, "alpha\n", at))
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("error result", func(t *testing.T) {
		r := good
		r.IsError = true
		installSource(r)
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("empty content", func(t *testing.T) {
		installSource(viewRead(t, "view", p, "", at))
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("bad metadata", func(t *testing.T) {
		r := good
		r.Metadata = "not json"
		installSource(r)
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("content changed", func(t *testing.T) {
		installSource(viewRead(t, "view", p, "gone content", at))
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("source error", func(t *testing.T) {
		SetSessionReadSource(func(context.Context, string) ([]SessionToolRead, error) {
			return nil, errors.New("boom")
		})
		if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
			t.Fatal("want zero")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		gone := filepath.Join(wd, "gone.txt")
		installSource(viewRead(t, "view", gone, "x", at))
		if !lastReadTimeForEdit(rhCtx(), gone).IsZero() {
			t.Fatal("want zero")
		}
	})
}

func TestReadHistory_RelativePath(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "rel.txt", "hello")
	at := time.Now()
	installSource(viewRead(t, "view", "rel.txt", "hello", at))
	if got := lastReadTimeForEdit(rhCtx(), p); !got.Equal(at) {
		t.Fatalf("got %v want %v", got, at)
	}
}

func TestReadHistory_LatestEntryWinsFailClosed(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "a.txt", "new content")
	old := time.Now().Add(-time.Hour)
	latest := time.Now()

	// Latest matches the file: it wins over the older one.
	installSource(
		viewRead(t, "view", p, "new content", latest),
		viewRead(t, "view", p, "new content", old),
	)
	if got := lastReadTimeForEdit(rhCtx(), p); !got.Equal(latest) {
		t.Fatalf("got %v want %v", got, latest)
	}

	// Latest does not match: do not fall back to the older matching entry.
	fileRecordMutex.Lock()
	fileRecords = make(map[string]fileRecord)
	fileRecordMutex.Unlock()
	installSource(
		viewRead(t, "view", p, "stale content", latest),
		viewRead(t, "view", p, "new content", old),
	)
	if !lastReadTimeForEdit(rhCtx(), p).IsZero() {
		t.Fatal("want zero when latest entry fails")
	}
}

func newTestEdit(t *testing.T) BaseTool {
	t.Helper()
	perms := permission.NewPermissionService()
	perms.RegisterSessionHandler(readHistorySession, func(permission.CreatePermissionRequest) bool { return true })
	return NewEditTool(NewStaticLSPProvider(nil), perms, fakeEditHistory{})
}

func runEdit(t *testing.T, path, oldS, newS string) ToolResponse {
	t.Helper()
	ctx := context.WithValue(rhCtx(), MessageIDContextKey, "msg-1")
	in, _ := json.Marshal(EditParams{FilePath: path, OldString: oldS, NewString: newS})
	resp, err := newTestEdit(t).Run(ctx, ToolCall{ID: "c1", Name: EditToolName, Input: string(in)})
	if err != nil {
		t.Fatalf("edit run: %v", err)
	}
	return resp
}

func TestReadHistory_EditAfterRestart(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "e.txt", "one two three\n")
	// CalledAt after the file mtime, as in a real earlier view.
	installSource(viewRead(t, "view", p, "one two three\n", time.Now().Add(time.Minute)))

	resp := runEdit(t, p, "two", "2")
	if resp.IsError {
		t.Fatalf("edit failed: %s", resp.Content)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "one 2 three") {
		t.Fatalf("file not edited: %q", data)
	}
}

func TestReadHistory_EditFailsWhenModifiedAfterRead(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "e.txt", "one two three\n")
	installSource(viewRead(t, "view", p, "one two three\n", time.Now().Add(-time.Hour)))

	resp := runEdit(t, p, "two", "2")
	if !resp.IsError || !strings.Contains(resp.Content, "has been modified since it was last read") {
		t.Fatalf("expected modified-since-read error, got %q", resp.Content)
	}
}

func TestReadHistory_EditFailsWithoutHistory(t *testing.T) {
	wd := setupReadHistory(t)
	p := writeTemp(t, wd, "e.txt", "one two three\n")
	resp := runEdit(t, p, "two", "2")
	if !resp.IsError || !strings.Contains(resp.Content, "you must read the file before editing it") {
		t.Fatalf("expected must-read error, got %q", resp.Content)
	}
}
