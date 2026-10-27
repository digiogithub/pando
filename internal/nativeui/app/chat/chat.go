// Package chat is the chat area of the native desktop: the sessions list,
// session history and streaming runs (P0 MVP; P1 brings parity with the Web
// UI: tool cards, diffs, questions, plan/goal, model picker, slash and @).
package chat

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

func init() {
	app.Register(app.Area{
		ID: "chat", Path: "/chat", Patterns: []string{"/chat", "/chat/s/{id}"},
		TitleKey: "nav.chat", Icon: kit.IconChat, Section: app.SectionMain, Order: 0,
		New: func(a *app.Context) app.View { return New(a) },
	})
	app.Register(app.Area{
		ID: "simple-chat", Path: "/chat/simple", Patterns: []string{"/chat/simple", "/chat/simple/s/{id}"},
		TitleKey: "nav.simpleChat", Icon: kit.IconSimpleChat, Section: app.SectionMain, Order: 1,
		New: func(a *app.Context) app.View { return NewSimple(a) },
	})
}

// streamMsg carries a stream event (or the end of a stream) to the UI.
type streamMsg struct {
	conv *Conversation
	tag  int
	ev   client.StreamEvent
	end  *streamEnd
}

type streamEnd struct {
	out client.StreamOutcome
	err error
}

// Chat is the chat area's state. All fields are owned by the UI thread.
type Chat struct {
	app *app.Context
	// simple selects the simple chat layout (conversation alone, trimmed
	// chrome), registered as the simple-chat area.
	simple bool

	sessions    []client.SessionSummary
	sessLoading bool
	sessErr     string
	sessList    ui.ListState
	selected    int
	sessTotal   int
	sessMore    bool
	sessSearch  string
	sessHidden  bool
	sessOffset  int
	rows        []sessRow

	// current is the open session id; "" is the new-chat draft (draftConv).
	current   string
	convs     map[string]*Conversation
	draftConv *Conversation
	draft     string

	md     markdown.Options
	events *app.Batch[streamMsg]

	renaming   bool
	renameID   string
	renameText string

	comp  composerState
	model modelState
	// modelFetching marks sessions whose model is being fetched.
	modelFetching map[string]bool
	persona       personaState

	qstate         map[string]*qState
	inline         map[string]*inlineDiff
	bitmaps        map[string]*ui.Bitmap
	copied         map[string]time.Time
	planOpen       bool
	filesCollapsed bool
	goalCancelling bool
	diff           *diffViewer
	infoOpen       bool
	tasks          []client.Task
	cwd            string
	connected      bool
	// pickedAuto remembers sessions whose auto-approve was fetched.
	autoFetched map[string]bool

	started bool
	stop    context.CancelFunc
	tagSeq  int

	// routeID is the session id of the route last seen; navPush makes the
	// next route update a Push (shellwire.go).
	routeID string
	navPush bool
}

// New returns the chat view.
func New(a *app.Context) *Chat {
	ch := &Chat{
		app: a, convs: map[string]*Conversation{}, selected: -1,
		qstate: map[string]*qState{}, inline: map[string]*inlineDiff{}, bitmaps: map[string]*ui.Bitmap{},
		copied: map[string]time.Time{}, modelFetching: map[string]bool{}, autoFetched: map[string]bool{},
		infoOpen: true,
	}
	ch.model.session = map[string]client.SessionModel{}
	ch.comp.histIdx = -1
	ch.events = app.NewBatch(a.Update, ch.applyEvents)
	ch.sessList.Key = func(i int) any { return ch.sessRowKey(i) }
	// Rows handle their own clicks: ListState.Selected would paint the
	// choice in the accent colour, which the row styles itself.
	// ch.md.OnLink stays nil: MyGo opens http(s)/mailto links in the
	// browser and ignores other schemes.
	return ch
}

// NewSimple returns the simple chat view: the conversation alone, with the
// sessions panel collapsible and no goal/plan banners.
func NewSimple(a *app.Context) *Chat {
	ch := New(a)
	ch.simple = true
	ch.infoOpen = false
	return ch
}

// Close stops background work.
func (ch *Chat) Close() {
	ch.forgetShown()
	if ch.stop != nil {
		ch.stop()
	}
	for _, cv := range ch.convs {
		if cv.cancel != nil {
			cv.cancel()
		}
	}
}

func (ch *Chat) start() {
	if ch.started {
		return
	}
	ch.started = true
	ch.hookShell()
	ctx, cancel := context.WithCancel(ch.app.Ctx)
	ch.stop = cancel
	if n := spikeMessages(); n > 0 {
		ch.loadSpike(n)
	}
	if ch.app.Client == nil {
		return
	}
	ch.connected = true
	ch.loadSessions(0)
	ch.app.Go(func(ctx context.Context) {
		if w, err := ch.app.Client.Workspace(ctx); err == nil {
			ch.app.Update(func() { ch.cwd = w.CWD })
		}
	})
	ch.app.Go(func(context.Context) { ch.poll(ctx) })
	ch.app.Go(func(context.Context) {
		err := ch.app.Client.SubscribeSessionEvents(ctx, func(ev client.SessionEvent) {
			ch.app.Update(func() { ch.sessionEvent(ev) })
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			ch.app.Log.Warn("session events stream ended", "error", err)
		}
	})
}

// sessionPage is the number of sessions fetched per page.
const sessionPage = 100

// loadSessions fetches a page of the sessions list (offset 0 is the first
// page; later ones come from "Load more").
func (ch *Chat) loadSessions(offset int) {
	ch.sessLoading = true
	ch.app.Go(func(ctx context.Context) {
		list, err := ch.app.Client.ListSessions(ctx, sessionPage, offset)
		ch.app.Update(func() {
			ch.sessLoading = false
			if err != nil {
				ch.sessErr = err.Error()
				return
			}
			ch.sessErr = ""
			ch.sessTotal, ch.sessMore = list.Total, list.HasMore
			ch.sessOffset = offset + len(list.Sessions)
			for _, s := range list.Sessions {
				ch.upsertSession(s)
			}
		})
	})
}

func (ch *Chat) sessionEvent(ev client.SessionEvent) {
	switch ev.Type {
	case "session_upsert":
		ch.upsertSession(ev.Session)
		if cv, ok := ch.convs[ev.ID]; ok {
			cv.BusyElsewhere = ev.Session.BusyElsewhere
			// Follow runs this client did not start (another surface, a
			// resurrected parent after a delegated task): the run_seq
			// protocol, driven by the global session stream.
			if cv.Loaded && !cv.Streaming && (ev.Session.IsRunning || ev.Session.RunSeq > cv.RunSeq) {
				ch.checkRun(cv)
			}
		}
	case "session_deleted":
		ch.removeSession(ev.ID)
	}
}

// checkRun asks the server whether a conversation this client is not
// streaming has a run: an active one is attached (replayed from its start),
// a finished one it has not seen reloads the history. This is the run_seq
// protocol of the GPUI client (follow.rs), fed by the session events stream
// and the poller of the open session.
func (ch *Chat) checkRun(cv *Conversation) {
	if cv.ID == "" || ch.app.Client == nil || cv.checking {
		return
	}
	cv.checking = true
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		p, err := ch.app.Client.Pending(ctx, id)
		// A run started elsewhere: its prompt is only in the history (the
		// stream replays the run, not the prompt), so fetch it before
		// trimming the transcript back to the last prompt.
		var detail client.SessionDetail
		if err == nil && p.Running {
			detail, err = ch.app.Client.GetSession(ctx, id)
		}
		ch.app.Update(func() {
			cv.checking = false
			if err != nil || cv.Streaming {
				return
			}
			cv.MergePending(p)
			switch {
			case p.Running:
				cv.SetHistory(detail.Messages)
				cv.TrimToLastPrompt()
				ch.attach(cv)
			case p.RunSeq > cv.RunSeq:
				cv.RunSeq = p.RunSeq
				ch.reload(cv)
			}
		})
	})
}

// poll checks the open session every few seconds for runs started
// elsewhere (the session events stream reports them only once they end),
// for prompts blocking the agent that a dropped stream never delivered
// (permissions, questions) and for the delegated tasks of the info panel.
func (ch *Chat) poll(ctx context.Context) {
	tick := time.NewTicker(4 * time.Second)
	defer tick.Stop()
	n := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			n++
			ch.app.Update(func() {
				cv, ok := ch.convs[ch.current]
				if ok && cv.Loaded && !cv.Streaming {
					ch.checkRun(cv)
				} else if ok && cv.Streaming {
					ch.fetchPending(cv)
				}
				if ch.infoOpen {
					ch.fetchTasks()
				}
			})
		}
	}
}

// fetchPending adopts the permission and question requests the server
// lists for a streaming conversation.
func (ch *Chat) fetchPending(cv *Conversation) {
	if cv.ID == "" || ch.app.Client == nil {
		return
	}
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		p, err := ch.app.Client.Pending(ctx, id)
		if err != nil {
			return
		}
		ch.app.Update(func() { cv.MergePending(p) })
	})
}

func (ch *Chat) fetchTasks() {
	ch.app.Go(func(ctx context.Context) {
		tasks, err := ch.app.Client.Tasks(ctx)
		if err == nil {
			ch.app.Update(func() { ch.tasks = tasks })
		}
	})
}

func (ch *Chat) upsertSession(s client.SessionSummary) {
	if s.ParentSessionID != "" {
		return // delegated sub-sessions are not listed (as in the Web UI)
	}
	for i := range ch.sessions {
		if ch.sessions[i].ID == s.ID {
			ch.sessions[i] = s
			ch.sortSessions()
			return
		}
	}
	ch.sessions = append(ch.sessions, s)
	ch.sortSessions()
}

func (ch *Chat) sortSessions() {
	sort.SliceStable(ch.sessions, func(i, j int) bool { return ch.sessions[i].UpdatedAt > ch.sessions[j].UpdatedAt })
	ch.selected = -1
	for i, s := range ch.sessions {
		if s.ID == ch.current {
			ch.selected = i
		}
	}
}

func (ch *Chat) removeSession(id string) {
	for i := range ch.sessions {
		if ch.sessions[i].ID == id {
			ch.sessions = append(ch.sessions[:i], ch.sessions[i+1:]...)
			break
		}
	}
	if cv := ch.convs[id]; cv != nil && cv.cancel != nil {
		cv.cancel()
	}
	delete(ch.convs, id)
	if ch.current == id {
		ch.newChat()
	}
	ch.sortSessions()
}

func (ch *Chat) session(id string) (client.SessionSummary, bool) {
	for _, s := range ch.sessions {
		if s.ID == id {
			return s, true
		}
	}
	return client.SessionSummary{}, false
}

// conv returns the open conversation, creating the draft for a new chat.
func (ch *Chat) conv() *Conversation {
	if ch.current == "" {
		if ch.draftConv == nil {
			ch.draftConv = newConversation("")
			ch.draftConv.Loaded = true
		}
		return ch.draftConv
	}
	cv, ok := ch.convs[ch.current]
	if !ok {
		cv = newConversation(ch.current)
		ch.convs[ch.current] = cv
	}
	return cv
}

func (ch *Chat) newChat() {
	ch.current = ""
	if ch.draftConv != nil && ch.draftConv.ID != "" {
		ch.draftConv = nil
	}
	ch.sortSessions()
}

// Open shows a session, loading its history the first time.
func (ch *Chat) Open(id string) {
	ch.current = id
	ch.sortSessions()
	cv := ch.conv()
	if cv.Loaded || cv.Loading || ch.app.Client == nil {
		return
	}
	cv.Loading = true
	ch.app.Go(func(ctx context.Context) {
		d, err := ch.app.Client.GetSession(ctx, id)
		ch.app.Update(func() {
			cv.Loading = false
			if err != nil {
				cv.LoadErr = err.Error()
				return
			}
			if cv.Streaming {
				// Our own run already streams into it: a history load now
				// would duplicate what the stream delivers.
				cv.Loaded = true
				return
			}
			cv.LoadErr = ""
			cv.SetHistory(d.Messages)
			cv.RunSeq = d.RunSeq
			cv.BusyElsewhere = d.BusyElsewhere
			ch.seedHistory(cv)
			if d.IsRunning && !d.BusyElsewhere {
				cv.TrimToLastPrompt()
				ch.attach(cv)
			}
			ch.hydrate(cv)
		})
	})
}

// seedHistory adds the prompts of a loaded transcript to the Up/Down
// recall, oldest first, without duplicating the tail.
func (ch *Chat) seedHistory(cv *Conversation) {
	if len(ch.comp.history) > 0 {
		return
	}
	for _, it := range cv.Items {
		if it.Kind == ItemUser && strings.TrimSpace(it.Text) != "" {
			ch.pushHistory(it.Text)
		}
	}
}

// hydrate loads what a session shows besides its messages: the goal, the
// auto-approve state, the pending prompts and the files agent-vcs saw it
// touch.
func (ch *Chat) hydrate(cv *Conversation) {
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		goal, _ := ch.app.Client.Goal(ctx, id)
		auto, _ := ch.app.Client.AutoApprove(ctx, id)
		pend, perr := ch.app.Client.Pending(ctx, id)
		vcs, _ := ch.app.Client.AgentVCSDiff(ctx, id)
		ch.app.Update(func() {
			if !cv.Streaming || cv.Goal == nil {
				cv.Goal = goal
			}
			cv.AutoApprove = auto
			if perr == nil {
				cv.MergePending(pend)
			}
			cv.MergeVCS(vcs)
		})
	})
}

func (ch *Chat) reload(cv *Conversation) {
	if cv.ID == "" || ch.app.Client == nil {
		return
	}
	ch.app.Go(func(ctx context.Context) {
		d, err := ch.app.Client.GetSession(ctx, cv.ID)
		if err != nil {
			return
		}
		ch.app.Update(func() {
			if cv.Streaming {
				return // never replace a transcript a stream is writing
			}
			cv.SetHistory(d.Messages)
			cv.RunSeq = max(cv.RunSeq, d.RunSeq)
			cv.BusyElsewhere = d.BusyElsewhere
			ch.hydrate(cv)
		})
	})
}

// newTag tags a stream so events of superseded streams are dropped.
func (ch *Chat) newTag(cv *Conversation) (int, context.Context) {
	ch.tagSeq++
	cv.streamTag = ch.tagSeq
	if cv.cancel != nil {
		cv.cancel()
	}
	ctx, cancel := context.WithCancel(ch.app.Ctx)
	cv.cancel = cancel
	return cv.streamTag, ctx
}

// attach follows a run this client did not start.
func (ch *Chat) attach(cv *Conversation) {
	if ch.app.Client == nil {
		return
	}
	cv.BeginAttach()
	tag, ctx := ch.newTag(cv)
	id := cv.ID
	ch.app.Go(func(context.Context) {
		out, err := ch.app.Client.AttachStream(ctx, id, func(ev client.StreamEvent) {
			ch.events.Push(streamMsg{conv: cv, tag: tag, ev: ev})
		})
		ch.events.Push(streamMsg{conv: cv, tag: tag, end: &streamEnd{out, err}})
	})
}

// Send sends the draft: a new run, or feedback steering the running one.
func (ch *Chat) Send(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || ch.app.Client == nil {
		return
	}
	cv := ch.conv()
	if cv.BusyElsewhere && !cv.Streaming {
		return // a subagent process runs it: a prompt would start a second run
	}
	ch.pushHistory(prompt)
	if cv.Running && cv.ID != "" {
		id := cv.ID
		streaming := cv.Streaming
		ch.app.Go(func(ctx context.Context) {
			err := ch.app.Client.Steer(ctx, id, prompt)
			ch.app.Update(func() {
				switch {
				case errors.Is(err, client.ErrNotBusy):
					// The run ended meanwhile: send it as a prompt (409 rule).
					cv.Running, cv.Streaming = false, false
					ch.startRun(cv, prompt)
				case err != nil:
					ch.app.Toasts.Error("Could not send feedback", err.Error())
				case !streaming:
					cv.add(&Item{Kind: ItemUser, Text: prompt, Done: true})
				}
			})
		})
		return
	}
	ch.startRun(cv, prompt)
}

func (ch *Chat) startRun(cv *Conversation, prompt string) {
	cv.BeginRun(prompt)
	tag, ctx := ch.newTag(cv)
	req := client.ChatRequest{SessionID: cv.ID, Prompt: prompt}
	if cv.ID == "" {
		if m := ch.nextModel(); m != "" {
			req.Model = m
			ch.setNextModel("")
		}
	}
	ch.app.Go(func(context.Context) {
		out, err := ch.app.Client.ChatStream(ctx, req, func(ev client.StreamEvent) {
			ch.events.Push(streamMsg{conv: cv, tag: tag, ev: ev})
		})
		ch.events.Push(streamMsg{conv: cv, tag: tag, end: &streamEnd{out, err}})
	})
}

// Stop cancels the active run.
func (ch *Chat) Stop() {
	cv := ch.conv()
	if cv.ID == "" {
		if cv.cancel != nil {
			cv.cancel()
		}
		cv.Finish()
		return
	}
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.Cancel(ctx, id); err != nil {
			ch.app.Toasts.Error("Could not stop the run", err.Error())
		}
	})
}

// applyEvents runs on the UI thread with every event queued since the last
// frame (one frame per batch, not per token).
func (ch *Chat) applyEvents(msgs []streamMsg) {
	for _, m := range msgs {
		cv := m.conv
		if m.tag != cv.streamTag {
			continue // a superseded stream
		}
		if m.end != nil {
			ch.endStream(cv, m.end)
			continue
		}
		if m.ev.Type == client.EventSession && m.ev.SessionID != "" && cv.ID == "" {
			cv.ID = m.ev.SessionID
			ch.convs[cv.ID] = cv
			if ch.current == "" && ch.draftConv == cv {
				ch.current = cv.ID
				ch.draftConv = nil
				ch.sortSessions()
			}
		}
		if m.ev.Type == client.EventSystemMessage {
			if r := m.ev.RoutingInfo(); r != nil && r.Model != "" {
				ch.model.lastRouted = r.Model
			}
		}
		if cv.Apply(m.ev) && m.ev.Type == client.EventDone {
			ch.reload(cv)
		}
		if m.ev.Type == client.EventDone && cv.ID != "" {
			ch.refreshPersona(cv.ID)
		}
	}
}

func (ch *Chat) endStream(cv *Conversation, end *streamEnd) {
	if !cv.Streaming {
		return // `done` already finished it
	}
	empty := cv.Finish()
	if end.err != nil && !errors.Is(end.err, context.Canceled) {
		cv.add(&Item{Kind: ItemError, Text: end.err.Error(), Done: true})
		return
	}
	if !end.out.Done && empty {
		ch.reload(cv)
	}
}

// Title is the header title: the open session's.
func (ch *Chat) Title() string {
	if ch.current == "" {
		return ch.app.T("nav.newSession")
	}
	if s, ok := ch.session(ch.current); ok && strings.TrimSpace(s.Title) != "" {
		return s.Title
	}
	return ch.app.T("nav.untitledSession")
}

// HeaderActions adds the context badge, panel toggles and new-session
// button.
func (ch *Chat) HeaderActions(c *ui.Context) {
	cv := ch.conv()
	if total, window := ch.sessionTokens(cv); window > 0 && total > 0 {
		kit.Badge(c, ch.app.T("chat.composer.context", "pct", fmt.Sprintf("%.0f", contextPct(total, window))), kit.ToneNeutral)
	}
	if ch.simple {
		if kit.IconButton(c, kit.IconPanelLeft, ch.app.T("chat.simple.toggleSessions")).Clicked() {
			ch.sessHidden = !ch.sessHidden
		}
	}
	if !ch.infoOpen {
		if kit.IconButton(c, kit.IconPanelRight, ch.app.T("chat.info.show")).Clicked() {
			ch.infoOpen = true
			ch.fetchTasks()
		}
	}
	if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Icon: kit.IconPlus, Label: ch.app.T("nav.newSession")}).Clicked() {
		ch.navPush = true
		ch.newChat()
	}
}

// Build shows the sessions pane, the conversation and the info panel.
func (ch *Chat) Build(c *ui.Context, r *ui.Route) {
	ch.start()
	ch.markShown()
	ch.syncRoute(r) // r is nil outside a router (tests, embedding)
	ch.takeDraft()
	t := kit.Current()
	ch.md.Theme = t.Markdown()
	cv := ch.conv()
	ch.shortcuts(c, cv)
	ui.Column(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if !(ch.simple && ch.sessHidden) {
				ch.sessionsPane(c, t)
			}
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
				if !ch.simple {
					ch.banners(c, t, cv)
				}
				ch.transcript(c, t, cv)
				ch.filesBar(c, t, cv)
				ch.composer(c, t, cv)
			})
			ch.infoSidebar(c, t, cv)
		})
		if ch.simple {
			ch.simpleFooter(c, t, cv)
		}
	})
	ch.renameDialog(c)
	ch.modelDialog(c, t, cv)
	if !ch.headerPersona() {
		ch.personaDialog(c, t)
	}
	ch.diffOverlay(c, t)
	ch.dialogs(c, t, cv)
}

// simpleFooter is the status bar of the simple chat: connection and the
// session's message and token counts.
func (ch *Chat) simpleFooter(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	ui.Row(c).Padding(4, 14).Gap(14).AlignItems(ui.Center).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Background(t.BgShell).Children(func() {
		dot, txt := t.Danger, ch.app.T("common.disconnected")
		if ch.connected {
			dot, txt = t.Success, ch.app.T("common.connected")
		}
		ui.Box(c).Width(7).Height(7).Radius(4).Background(dot)
		ui.Text(c, txt).FontSize(t.TextXS).TextColor(t.FgMuted)
		ui.Spacer(c)
		if s, ok := ch.session(cv.ID); ok && s.PromptTokens+s.CompletionTokens > 0 {
			ui.Text(c, fmt.Sprintf("%d %s · %d %s", s.MessageCount, ch.app.T("common.messages"),
				s.PromptTokens+s.CompletionTokens, ch.app.T("common.tokens"))).FontSize(t.TextXS).TextColor(t.FgMuted)
		}
	})
}

func (ch *Chat) transcript(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	list := ui.List(c.Key("transcript:"+cv.keyID()), &cv.List, len(cv.Items), func(i int) {
		ch.item(c, t, cv, cv.Items[i])
	}).Grow(1).Padding(20, 24).Gap(14).Justify(ui.End)
	list.Children(func() {
		switch {
		case cv.Loading:
			ui.Row(c).Margin(ui.Auto).Children(func() { kit.Spinner(c).FontSize(20).TextColor(t.FgFaint) })
		case cv.LoadErr != "":
			kit.EmptyState(c, kit.IconError, "Could not load the session", cv.LoadErr)
		case len(cv.Items) == 0:
			ch.emptyHead(c, t)
		}
	})
	if !cv.List.AtEnd() && len(cv.Items) > 0 {
		ui.Row(c).Justify(ui.Center).Margin(-44, 0, 8, 0).Children(func() {
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Outline, Size: kit.SizeSM, Icon: kit.IconArrowDown,
				Label: ch.app.T("chat.scrollToBottom")}).Background(t.BgOverlay).Clicked() {
				cv.List.ScrollToEnd()
			}
		})
	}
}

// emptyHead is the empty conversation: the title and prompt suggestions.
func (ch *Chat) emptyHead(c *ui.Context, t *kit.Tokens) {
	kit.EmptyState(c, kit.IconBot, ch.app.T("chat.empty.title"), ch.app.T("chat.composer.hintIdle"))
	ui.Row(c).Gap(8).Justify(ui.Center).Wrap().Padding(0, 24, 24, 24).Children(func() {
		for _, k := range []string{"explain", "bug", "tests", "review"} {
			if kit.Button(c.Key("sg"+k), kit.ButtonOpts{Variant: kit.Outline, Size: kit.SizeSM,
				Label: ch.app.T("chat.empty.suggestions." + k)}).Clicked() {
				ch.draft = ch.app.T("chat.empty.prompts." + k)
			}
		}
	})
}

func (cv *Conversation) keyID() string {
	if cv.ID == "" {
		return fmt.Sprintf("draft-%p", cv)
	}
	return cv.ID
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (truncated)"
}
