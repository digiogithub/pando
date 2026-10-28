package chat

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
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

// A new chat centers its greeting, the composer and the suggestions
// vertically; once it has messages the composer goes back to the bottom.
func TestEmptyChatCenteredVertically(t *testing.T) {
	const h = 900
	ch, tt := newWideChat(t, 1200, h)
	cv := newConversation("")
	cv.Loaded = true
	ch.draftConv, ch.current = cv, ""
	tt.Frame()
	title, ok := tt.Find(ch.app.T("chat.empty.title"))
	sugg, ok2 := tt.Find(ch.app.T("chat.empty.suggestions.explain"))
	if !ok || !ok2 {
		t.Fatalf("empty state missing: %q", tt.Texts())
	}
	if title.Y < h*0.15 || sugg.Y > h*0.85 || title.Y > sugg.Y {
		t.Errorf("not centered: title y=%v, suggestions y=%v in %v", title.Y, sugg.Y, h)
	}
	if mid := (title.Y + sugg.Y + sugg.H) / 2; mid < h*0.35 || mid > h*0.65 {
		t.Errorf("block middle at %v, want near %v", mid, h/2)
	}
}

// The info panel shows the running version and a newer release.
func TestInfoPanelShowsVersionUpdate(t *testing.T) {
	ch, tt := newWideChat(t, 1400, 900)
	ch.infoOpen, ch.verFetched = true, true
	ch.version = &client.VersionStatus{Version: "1.5.0", Latest: "1.6.0", UpdateAvailable: true, UpdateCommand: "pando update"}
	tt.Frame()
	for _, s := range []string{"Pando 1.5.0", ch.app.T("version.updateAvailable", "latest", "1.6.0"), "pando update"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q: %q", s, tt.Texts())
		}
	}
	ch.version.UpdateAvailable = false
	tt.Frame()
	if tt.HasText("pando update") {
		t.Error("no update: no command")
	}
}
