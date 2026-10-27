package chat

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/egoist/mygo/ui"
)

func newWideChat(t *testing.T, w, h int) (*Chat, *ui.Tester) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := app.NewContext(ctx, nil)
	ch := New(a)
	cv := newConversation("wide")
	cv.Loaded = true
	cv.add(&Item{Kind: ItemUser, Text: "Question one", Done: true})
	it := cv.add(&Item{Kind: ItemAssistant, Done: true})
	cv.newPart(it, PText).Text = "Answer one"
	cv.runStart = len(cv.Items)
	ch.convs[cv.ID] = cv
	ch.current = cv.ID
	ch.started = true
	ch.infoOpen = false
	ch.sessHidden = true
	tt := ui.NewTester(func(c *ui.Context) { ch.Build(c, nil) }, w, h)
	tt.Frame()
	return ch, tt
}

// On a very wide window the transcript is a centered column of the web's
// width: the user bubble and the reply are never far apart.
func TestTranscriptCenteredOnWideWindow(t *testing.T) {
	const w = 2400
	_, tt := newWideChat(t, w, 800)
	q, ok1 := tt.Find("Question one")
	a, ok2 := tt.Find("Answer one")
	if !ok1 || !ok2 {
		t.Fatalf("texts missing: %q", tt.Texts())
	}
	left, right := (float32(w)-readingWidth)/2, (float32(w)+readingWidth)/2
	if a.X < left || a.X > left+gutter+40 {
		t.Errorf("assistant text at x=%v, want near the column's left edge %v", a.X, left+gutter)
	}
	if q.X+q.W > right-gutter+1 || q.X+q.W < right-gutter-60 {
		t.Errorf("user bubble text ends at x=%v, want near the column's right edge %v", q.X+q.W, right-gutter)
	}
	if gap := (q.X + q.W) - a.X; gap > textWidth {
		t.Errorf("user and assistant %v apart, more than the %v text column", gap, textWidth)
	}
	// A narrow window keeps using all of its width.
	tt.SetSize(500, 800)
	tt.Frame()
	if a2, _ := tt.Find("Answer one"); a2.X > 2*gutter+20 {
		t.Errorf("narrow window: assistant text at x=%v", a2.X)
	}
}

// The composer sits in the same centered column.
func TestComposerCenteredOnWideWindow(t *testing.T) {
	const w = 2400
	_, tt := newWideChat(t, w, 800)
	r, ok := tt.Find("Message Pando…")
	if !ok {
		for _, s := range tt.Texts() {
			if len(s) > 3 {
				if rr, ok2 := tt.Find(s); ok2 && rr.Y > 650 {
					r, ok = rr, true
					break
				}
			}
		}
	}
	if !ok {
		t.Fatalf("composer text not found: %q", tt.Texts())
	}
	if left := (float32(w) - readingWidth) / 2; r.X < left {
		t.Errorf("composer text at x=%v, left of the column %v", r.X, left)
	}
}

// The sessions toggle lives in the sidebar (app.SidebarPanel): on the icon
// rail it shows or hides the sessions column beside the chat; the expanded
// sidebar lists the sessions itself and the chat drops its column.
func TestSessionsPaneToggle(t *testing.T) {
	ch, _ := newWideChat(t, 1200, 700)
	ch.sessHidden = false
	collapsed := true
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Fill().Children(func() {
			ui.Column(c).Width(216).Children(func() { ch.SidebarPanel(c, collapsed) })
			ui.Column(c).Grow(1).Children(func() { ch.Build(c, nil) })
		})
	}, 1400, 700)
	tt.Frame()
	if !tt.HasText("Sessions") || ch.sessHosted {
		t.Fatalf("icon rail: the chat must show its sessions column: %q", tt.Texts())
	}
	if err := tt.Click(ch.app.T("chat.simple.toggleSessions")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !ch.sessHidden || tt.HasText("Sessions") {
		t.Fatal("the rail toggle did not hide the sessions column")
	}
	// Expanded sidebar: the sessions move into it.
	collapsed = false
	tt.Frame()
	if err := tt.Click(ch.app.T("chat.simple.toggleSessions")); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if ch.sessHidden || !ch.sessHosted {
		t.Fatalf("expanded sidebar: hidden=%v hosted=%v", ch.sessHidden, ch.sessHosted)
	}
	if !tt.HasText("SESSIONS") || tt.HasText("Sessions") {
		t.Errorf("sessions must be listed by the sidebar only: %q", tt.Texts())
	}
}
