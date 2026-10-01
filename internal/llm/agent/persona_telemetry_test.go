package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/extevents"
	"github.com/digiogithub/pando/pkg/extension"
)

func TestPersonaTelemetryPrivacy(t *testing.T) {
	const marker = "SECRET-PROMPT-MARKER"
	const apiKey = "sk-router-secret-key-456"
	env := newPersonaEnv(t, true, withAutoConfig(func(m *config.ModelAutoModeConfig) {
		m.Enabled = false
		m.Router.APIKey = apiKey
	}))
	sid := env.personaSession("p-telemetry")

	logs := &logCapture{}
	prevLogger := slog.Default()
	slog.SetDefault(slog.New(logs))
	t.Cleanup(func() { slog.SetDefault(prevLogger) })

	var mu sync.Mutex
	var captured []extension.Event
	ctx, cancel := context.WithCancel(context.Background())
	extevents.SetSink(ctx, func(ev extension.Event) {
		mu.Lock()
		captured = append(captured, ev)
		mu.Unlock()
	})
	t.Cleanup(func() {
		cancel()
		extevents.SetSink(context.Background(), nil)
	})

	env.answerPersona("qa", 0.9)
	env.run(sid, "write a test plan "+marker)
	env.run(sid, "and again "+marker)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(captured)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	payloads, _ := json.Marshal(captured)
	var personaEvents []extension.Event
	for _, ev := range captured {
		if ev.Topic == extension.TopicPersonaRoute {
			personaEvents = append(personaEvents, ev)
		}
	}
	mu.Unlock()
	if len(personaEvents) != 2 {
		t.Fatalf("PersonaRouted events = %d, want one per decision", len(personaEvents))
	}
	first := personaEvents[0]
	if first.Payload["persona"] != "qa" || first.Payload["source"] != PersonaSourceDecision || first.Payload["changed"] != true {
		t.Fatalf("event = %+v", first)
	}
	if personaEvents[1].Payload["changed"] != false {
		t.Fatalf("second event must report no change: %+v", personaEvents[1])
	}

	logs.mu.Lock()
	logText := strings.Join(logs.lines, "\n")
	logs.mu.Unlock()
	var logged bool
	for _, line := range strings.Split(logText, "\n") {
		if strings.Contains(line, "persona_auto: decision") {
			logged = true
			if !strings.HasPrefix(line, "INFO ") {
				t.Fatalf("decision must be logged at Info: %q", line)
			}
		}
	}
	if !logged {
		t.Fatalf("no Info decision line in logs:\n%s", logText)
	}
	for _, leak := range []string{marker, apiKey} {
		if strings.Contains(string(payloads), leak) {
			t.Errorf("event payloads leak %q", leak)
		}
		if strings.Contains(logText, leak) {
			t.Errorf("logs leak %q", leak)
		}
	}
}
