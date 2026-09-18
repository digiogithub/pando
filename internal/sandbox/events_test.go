package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// withEventsTestDir points config.Get().Data.Directory at a fresh temp dir
// for the duration of the test and resets every piece of process-wide event
// state (ring buffer, counters, once-guards, JSONL writer) before and after,
// so tests never see another test's events.
func withEventsTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := config.Get()
	config.SetForTests(&config.Config{Data: config.Data{Directory: dir}})
	ResetEventsForTests()
	t.Cleanup(func() {
		ResetEventsForTests()
		config.SetForTests(prev)
	})
	return dir
}

func TestEmitRedactsCommandSecrets(t *testing.T) {
	withEventsTestDir(t)

	const fakeKey = "sk-ant-abcdefghijklmnopqrstuvwxyz0123456789"
	command := "curl -H 'Authorization: Bearer " + fakeKey + "' https://api.example.com"

	Emit(context.Background(), Event{
		Type:      EventDenied,
		SessionID: "sess-1",
		Backend:   BackendLandlock,
		Mode:      string(ModeWorkspaceWrite),
		Command:   command,
	})
	CloseEventWriter()

	events := RecentEvents()
	if len(events) != 1 {
		t.Fatalf("RecentEvents() len = %d, want 1", len(events))
	}
	got := events[0].Command
	if strings.Contains(got, fakeKey) {
		t.Fatalf("Event.Command leaked the raw key: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("Event.Command = %q, want a [REDACTED] marker", got)
	}

	// The JSONL line on disk must not contain the raw key either.
	data, err := os.ReadFile(filepath.Join(config.Get().Data.Directory, eventsFileName))
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	if strings.Contains(string(data), fakeKey) {
		t.Fatalf("sandbox-events.jsonl leaked the raw key:\n%s", data)
	}
	if !strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("sandbox-events.jsonl missing a [REDACTED] marker:\n%s", data)
	}
}

func TestEmitUnavailableOncePerProcess(t *testing.T) {
	withEventsTestDir(t)

	for i := 0; i < 3; i++ {
		Emit(context.Background(), Event{
			Type:    EventUnavailable,
			Backend: BackendNone,
			Reason:  "windows: not supported",
		})
	}
	CloseEventWriter()

	if got := EventCounters().Unavailable; got != 1 {
		t.Fatalf("Unavailable counter = %d, want 1 (emitted once per process)", got)
	}
	n := 0
	for _, e := range RecentEvents() {
		if e.Type == EventUnavailable {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("RecentEvents() has %d EventUnavailable entries, want 1", n)
	}
}

func TestEventCounters(t *testing.T) {
	withEventsTestDir(t)

	Emit(context.Background(), Event{Type: EventApplied, Backend: BackendSeatbelt})
	Emit(context.Background(), Event{Type: EventDenied, Backend: BackendSeatbelt})
	Emit(context.Background(), Event{Type: EventDenied, Backend: BackendSeatbelt})
	Emit(context.Background(), Event{Type: EventEscalationRequested, Backend: BackendSeatbelt})
	Emit(context.Background(), Event{Type: EventEscalationGranted, Backend: BackendSeatbelt})
	Emit(context.Background(), Event{Type: EventEscalationDenied, Backend: BackendSeatbelt})

	c := EventCounters()
	if c.Applied != 1 || c.Denied != 2 || c.EscalationRequested != 1 || c.EscalationGranted != 1 || c.EscalationDenied != 1 {
		t.Fatalf("EventCounters() = %+v, unexpected", c)
	}
}

func TestRecentEventsCapsRingBuffer(t *testing.T) {
	withEventsTestDir(t)

	for i := 0; i < maxRingEvents+50; i++ {
		Emit(context.Background(), Event{Type: EventDenied, Backend: BackendLandlock, Path: filepath.Join("/tmp", "x")})
	}
	CloseEventWriter()

	events := RecentEvents()
	if len(events) != maxRingEvents {
		t.Fatalf("RecentEvents() len = %d, want %d", len(events), maxRingEvents)
	}
}

func TestRedactCommandTruncates(t *testing.T) {
	long := strings.Repeat("a", maxEventCommand*2)
	got := redactCommand(long)
	if len(got) > maxEventCommand+len("…[truncated]") {
		t.Fatalf("redactCommand did not bound length: got %d bytes", len(got))
	}
	if redactCommand("") != "" {
		t.Fatal("redactCommand(\"\") should stay empty")
	}
}

func TestEmitSpawn(t *testing.T) {
	withEventsTestDir(t)
	p := Policy{Mode: ModeWorkspaceWrite}
	enforced := Capability{Backend: BackendLandlock, Enforced: true}

	EmitSpawn(context.Background(), PurposeSkills, false, p, enforced)
	EmitSpawn(context.Background(), PurposeSkills, true, Policy{Mode: ModeOff}, enforced)
	if n := len(RecentEvents()); n != 0 {
		t.Fatalf("uncovered/disabled spawns recorded %d events, want 0", n)
	}

	EmitSpawn(context.Background(), PurposeSkills, true, p, enforced)
	EmitSpawn(context.Background(), PurposeMCP, true, p, Capability{Backend: BackendNone, Reason: "no kernel support"})
	EmitSpawn(context.Background(), PurposeMCP, true, p, Capability{Backend: BackendNone, Reason: "no kernel support"})
	events := RecentEvents()
	if len(events) != 2 {
		t.Fatalf("events = %+v, want applied + one unavailable", events)
	}
	if events[0].Type != EventApplied || events[0].Reason != "purpose=skills" || events[0].Command != "" {
		t.Errorf("applied event = %+v", events[0])
	}
	if events[1].Type != EventUnavailable || events[1].Reason != "no kernel support" {
		t.Errorf("unavailable event = %+v", events[1])
	}
}
