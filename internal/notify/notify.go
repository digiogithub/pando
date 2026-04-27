package notify

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/pubsub"
)

// Level represents the severity of a notification.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Source identifies the component that emitted the notification.
type Source string

const (
	SourceLLMProvider Source = "llm_provider"
	SourceTool        Source = "tool"
	SourceLSP         Source = "lsp"
	SourceAgent       Source = "agent"
	SourceSystem      Source = "system"
)

// Notification is a user-facing event (not a debug log) that should be surfaced
// in the TUI, the Web UI via SSE, and the ACP protocol.
type Notification struct {
	ID      string        `json:"id"`
	Time    time.Time     `json:"time"`
	Level   Level         `json:"level"`
	Source  Source        `json:"source"`
	Message string        `json:"message"`
	TTL     time.Duration `json:"ttl,omitempty"` // 0 = permanent until dismissed
}

type bus struct {
	*pubsub.Broker[Notification]
}

var (
	defaultBus *bus
	once       sync.Once
)

func init() {
	once.Do(func() {
		defaultBus = &bus{Broker: pubsub.NewBroker[Notification]()}
	})
}

func publish(level Level, source Source, msg string, ttl time.Duration) {
	n := Notification{
		ID:      fmt.Sprintf("%d", time.Now().UnixNano()),
		Time:    time.Now(),
		Level:   level,
		Source:  source,
		Message: msg,
		TTL:     ttl,
	}
	defaultBus.Publish(pubsub.CreatedEvent, n)
}

// Info publishes an informational notification with a TTL after which it can be
// automatically dismissed.
func Info(source Source, msg string, ttl time.Duration) {
	publish(LevelInfo, source, msg, ttl)
}

// Warn publishes a warning notification with a TTL.
func Warn(source Source, msg string, ttl time.Duration) {
	publish(LevelWarn, source, msg, ttl)
}

// Error publishes a permanent error notification (TTL=0, dismissed manually).
func Error(source Source, msg string) {
	publish(LevelError, source, msg, 0)
}

// Subscribe returns a channel that receives Notification events while ctx is live.
func Subscribe(ctx context.Context) <-chan pubsub.Event[Notification] {
	return defaultBus.Subscribe(ctx)
}
