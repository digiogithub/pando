package chat

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/egoist/mygo/ui"
)

// TestRealServerOwnRun drives the chat view headlessly against a real Pando
// server with a configured model: PANDO_NATIVE_IT_URL=http://localhost:8765
// go test -run TestRealServerOwnRun ./internal/nativeui/app/chat -v
func TestRealServerOwnRun(t *testing.T) {
	url := os.Getenv("PANDO_NATIVE_IT_URL")
	if url == "" {
		t.Skip("PANDO_NATIVE_IT_URL not set")
	}
	c, err := client.New(client.Options{BaseURL: url})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := app.NewContext(ctx, c)
	q := &app.QueueDispatcher{}
	a.SetDispatcher(q)
	ch := New(a)
	defer ch.Close()
	frames := 0
	tt := ui.NewTester(func(c *ui.Context) { frames++; ch.Build(c, nil) }, 1200, 800)

	prompt := "Reply with a two item markdown list: alpha, beta. No tools."
	if p := os.Getenv("PANDO_NATIVE_IT_PROMPT"); p != "" {
		prompt = p
	}
	tt.Type(prompt)
	tt.Key(0, ui.KeyEnter)
	start := time.Now()
	var cv *Conversation
	batches := 0
	stopped := false
	for time.Since(start) < 3*time.Minute {
		if n := q.Drain(); n > 0 {
			batches += n
			tt.Frame()
		}
		cv = ch.conv()
		if os.Getenv("PANDO_NATIVE_IT_CANCEL") != "" && !stopped && cv.content {
			stopped = true
			ch.Stop() // cancel mid-stream: the server answers error + done
		}
		if cv.ID != "" && !cv.Running && len(cv.Items) > 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cv == nil || cv.Running {
		t.Fatalf("run did not finish; texts %q", tt.Texts())
	}
	var text string
	for _, it := range cv.Items {
		if it.Kind == ItemAssistant {
			text += it.Text
		}
		if it.Kind == ItemError {
			if stopped {
				t.Logf("error row after stop: %s", it.Text)
			} else {
				t.Errorf("error row: %s", it.Text)
			}
		}
	}
	t.Logf("session %s finished in %v: %d UI updates, %d frames, usage %+v", cv.ID, time.Since(start), batches, frames, cv.Usage)
	t.Logf("assistant text: %q", text)
	if text == "" && !stopped {
		t.Errorf("no assistant text; texts %q", tt.Texts())
	}
	if !cv.List.AtEnd() {
		t.Error("transcript not following the end")
	}
}
