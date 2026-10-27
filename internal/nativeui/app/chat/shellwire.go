package chat

import (
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// The chat's wiring to the window shell: the persona selector in the header
// (as the Web UI's Header), the model switcher overlay (Shell.NextModel and
// Shell.ModelChanged), Shift+Tab auto-approve, the design draft and the
// route of the open session.

func init() {
	app.RegisterHeaderSlot(app.HeaderSlot{ID: "persona", Order: 10, Build: func(c *ui.Context, a *app.Context) {
		ch := shownChat(a)
		if ch == nil {
			return
		}
		t := kit.Current()
		ch.personaChip(c, t)
		ch.personaDialog(c, t)
	}})
}

var (
	shownMu sync.Mutex
	// shown is, per window context, the chat built last (main or simple):
	// the header's persona selector follows its session.
	shown = map[*app.Context]*Chat{}
)

func shownChat(a *app.Context) *Chat {
	shownMu.Lock()
	defer shownMu.Unlock()
	return shown[a]
}

func (ch *Chat) markShown() {
	if ch.app.IsChild() {
		return
	}
	shownMu.Lock()
	shown[ch.app] = ch
	shownMu.Unlock()
}

func (ch *Chat) forgetShown() {
	shownMu.Lock()
	if shown[ch.app] == ch {
		delete(shown, ch.app)
	}
	shownMu.Unlock()
}

// headerPersona reports whether the persona selector lives in the window
// header (main window) rather than in the composer (project workspaces,
// whose embedded roots have no header slots).
func (ch *Chat) headerPersona() bool { return !ch.app.IsChild() }

// nextModel is the model chosen for the next new chat: the window's
// Shell().NextModel (shared with the model switcher overlay), or the chat's
// own in a project workspace.
func (ch *Chat) nextModel() string {
	if ch.app.IsChild() {
		return ch.model.pending
	}
	return ch.app.Shell().NextModel
}

func (ch *Chat) setNextModel(id string) {
	if ch.app.IsChild() {
		ch.model.pending = id
		return
	}
	ch.app.Shell().NextModel = id
}

// hookShell chains the chat into Shell().ModelChanged (once, at start).
func (ch *Chat) hookShell() {
	if ch.app.IsChild() {
		return
	}
	sh := ch.app.Shell()
	prev := sh.ModelChanged
	sh.ModelChanged = func(model, sessionID string) {
		if prev != nil {
			prev(model, sessionID)
		}
		ch.modelChanged(model, sessionID)
	}
}

// modelChanged follows a model picked in the switcher overlay.
func (ch *Chat) modelChanged(model, sessionID string) {
	ms := &ch.model
	auto := model == autoModelID
	switch {
	case sessionID != "":
		ms.session[sessionID] = client.SessionModel{SessionID: sessionID, Model: model, Override: true, AutoSelected: auto}
	case ch.app.Shell().NextModel == "":
		// No default model: the server persisted it as the default.
		ms.def, ms.auto = model, auto
	}
	for _, m := range ms.models {
		if m.ID == model && m.ContextWindow > 0 {
			if cv := ch.convs[sessionID]; cv != nil {
				cv.ContextWindow = m.ContextWindow
			}
		}
	}
}

// basePath is the area's route ("/chat" or "/chat/simple").
func (ch *Chat) basePath() string {
	if ch.simple {
		return "/chat/simple"
	}
	return "/chat"
}

// syncRoute keeps the open session and the route in step: a route change
// (deep link, back/forward) opens its session, and a session opened from the
// UI (click, new chat, a new chat's first prompt) updates the route, pushed
// for clicks so Back returns to the previous one.
func (ch *Chat) syncRoute(r *ui.Route) {
	if r == nil || ch.app.Router == nil {
		return
	}
	id := r.Param("id")
	if id != ch.routeID {
		ch.routeID = id
		if id != "" && id != ch.current {
			ch.Open(id)
		}
		return
	}
	if ch.current == id {
		ch.navPush = false
		return
	}
	target := ch.basePath()
	if ch.current != "" {
		target += "/s/" + ch.current
	}
	ch.routeID = ch.current
	if ch.navPush {
		ch.app.Router.Push(target)
	} else {
		ch.app.Router.Replace(target)
	}
	ch.navPush = false
}

// openFromUI opens a session chosen in the chat's own UI.
func (ch *Chat) openFromUI(id string) {
	ch.navPush = true
	ch.Open(id)
}

// takeDraft appends the text other areas left for the composer (Design
// Studio "Ask about this element", template "Try it").
func (ch *Chat) takeDraft() {
	if ch.app.PeekChatDraft() == "" {
		return
	}
	d := ch.app.TakeChatDraft()
	if strings.TrimSpace(ch.draft) != "" {
		ch.draft = strings.TrimRight(ch.draft, "\n") + "\n" + d
	} else {
		ch.draft = d
	}
}

// shortcuts handles Shift+Tab (toggle auto-approve of the open session), as
// the Web UI does.
func (ch *Chat) shortcuts(c *ui.Context, cv *Conversation) {
	if c.Shortcut(ui.Shift, ui.KeyTab) {
		ch.toggleAutoApprove(cv)
	}
}

func (ch *Chat) toggleAutoApprove(cv *Conversation) {
	if cv.ID == "" {
		return
	}
	ch.setAutoApprove(cv, !cv.AutoApprove)
}
