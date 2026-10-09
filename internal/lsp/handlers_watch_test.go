package lsp

import (
	"encoding/json"
	"testing"

	"github.com/digiogithub/pando/internal/lsp/protocol"
)

func registerWatchParams(t *testing.T, pattern string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"registrations": []map[string]any{{
			"id":     "r1",
			"method": "workspace/didChangeWatchedFiles",
			"registerOptions": map[string]any{
				"watchers": []map[string]any{{"globPattern": pattern}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Registrations must reach the handler of the client that received them, not
// the most recently created one.
func TestFileWatchRegistrationRoutedPerClient(t *testing.T) {
	a, b := &Client{}, &Client{}
	var gotA, gotB int
	a.SetFileWatchHandler(func(string, []protocol.FileSystemWatcher) { gotA++ })
	b.SetFileWatchHandler(func(string, []protocol.FileSystemWatcher) { gotB++ })

	if _, err := HandleRegisterCapability(a, registerWatchParams(t, "**/*.go")); err != nil {
		t.Fatal(err)
	}
	if gotA != 1 || gotB != 0 {
		t.Fatalf("routing wrong: a=%d b=%d", gotA, gotB)
	}
}

// Registrations arriving before a handler exists are replayed on SetFileWatchHandler.
func TestFileWatchRegistrationReplayedWhenHandlerSet(t *testing.T) {
	c := &Client{}
	if _, err := HandleRegisterCapability(c, registerWatchParams(t, "**/*.rs")); err != nil {
		t.Fatal(err)
	}
	var got []string
	c.SetFileWatchHandler(func(id string, w []protocol.FileSystemWatcher) { got = append(got, id) })
	if len(got) != 1 || got[0] != "r1" {
		t.Fatalf("pending registration not replayed: %v", got)
	}
	// Clearing the handler must not replay or panic.
	c.SetFileWatchHandler(nil)
}
