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

	sessions    []client.SessionSummary
	sessLoading bool
	sessErr     string
	sessList    ui.ListState
	selected    int

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

	started bool
	stop    context.CancelFunc
	tagSeq  int
}

// New returns the chat view.
func New(a *app.Context) *Chat {
	ch := &Chat{app: a, convs: map[string]*Conversation{}, selected: -1}
	ch.events = app.NewBatch(a.Update, ch.applyEvents)
	ch.sessList.Key = func(i int) any {
		if i < len(ch.sessions) {
			return ch.sessions[i].ID
		}
		return i
	}
	// Rows handle their own clicks: ListState.Selected would paint the
	// choice in the accent colour, which the row styles itself.
	// ch.md.OnLink stays nil: MyGo opens http(s)/mailto links in the
	// browser and ignores other schemes.
	return ch
}

// Close stops background work.
func (ch *Chat) Close() {
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
	ctx, cancel := context.WithCancel(ch.app.Ctx)
	ch.stop = cancel
	if n := spikeMessages(); n > 0 {
		ch.loadSpike(n)
	}
	if ch.app.Client == nil {
		return
	}
	ch.sessLoading = true
	ch.app.Go(func(context.Context) {
		list, err := ch.app.Client.ListSessions(ctx, 100, 0)
		ch.app.Update(func() {
			ch.sessLoading = false
			if err != nil {
				ch.sessErr = err.Error()
				return
			}
			ch.sessErr = ""
			for _, s := range list.Sessions {
				ch.upsertSession(s)
			}
		})
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

func (ch *Chat) sessionEvent(ev client.SessionEvent) {
	switch ev.Type {
	case "session_upsert":
		ch.upsertSession(ev.Session)
		// Follow runs this client did not start (another surface, a
		// resurrected parent after a delegated task): the run_seq protocol,
		// driven by the global session stream instead of polling.
		if cv, ok := ch.convs[ev.ID]; ok && cv.Loaded && !cv.Streaming && (ev.Session.IsRunning || ev.Session.RunSeq > cv.RunSeq) {
			ch.checkRun(cv)
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
// elsewhere (the session events stream reports them only once they end).
func (ch *Chat) poll(ctx context.Context) {
	tick := time.NewTicker(4 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			ch.app.Update(func() {
				if cv, ok := ch.convs[ch.current]; ok && cv.Loaded && !cv.Streaming {
					ch.checkRun(cv)
				}
			})
		}
	}
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
			if d.IsRunning && !d.BusyElsewhere {
				cv.TrimToLastPrompt()
				ch.attach(cv)
			}
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
		if cv.Apply(m.ev) && m.ev.Type == client.EventDone {
			ch.reload(cv)
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

// HeaderActions adds the new-session button.
func (ch *Chat) HeaderActions(c *ui.Context) {
	cv := ch.conv()
	if cv.Usage != nil && cv.Usage.ContextWindow > 0 {
		pct := 100 * float64(cv.Usage.PromptTokens+cv.Usage.CompletionTokens) / float64(cv.Usage.ContextWindow)
		kit.Badge(c, ch.app.T("chat.composer.context", "pct", fmt.Sprintf("%.0f", pct)), kit.ToneNeutral)
	}
	if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Icon: kit.IconPlus, Label: ch.app.T("nav.newSession")}).Clicked() {
		ch.newChat()
	}
}

// Build shows the sessions pane and the conversation.
func (ch *Chat) Build(c *ui.Context, r *ui.Route) {
	ch.start()
	if r == nil {
		// Built outside a router (tests, embedding).
	} else if id := r.Param("id"); id != "" && id != ch.current {
		ch.Open(id)
	}
	t := kit.Current()
	ch.md.Theme = t.Markdown()
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ch.sessionsPane(c, t)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			ch.transcript(c, t)
			ch.composer(c, t)
		})
	})
	ch.renameDialog(c)
}

func (ch *Chat) sessionsPane(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Width(260).Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Background(t.BgShell).
		AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Padding(10, 12, 6, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ch.app.T("nav.sections.sessions")).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgFaint).Grow(1)
			if ch.sessLoading {
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
			}
		})
		if ch.sessErr != "" {
			ui.Text(c, ch.sessErr).FontSize(t.TextXS).TextColor(t.Danger).Padding(4, 12)
		}
		list := ui.List(c, &ch.sessList, len(ch.sessions), func(i int) {
			ch.sessionRow(c, t, ch.sessions[i], i == ch.selected)
		}).Grow(1).Padding(0, 6, 8, 6).Gap(1)
		list.Children(func() {
			if len(ch.sessions) == 0 && !ch.sessLoading {
				ui.Text(c, ch.app.T("nav.noSessionsYet")).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(12)
			}
		})
	})
}

func (ch *Chat) sessionRow(c *ui.Context, t *kit.Tokens, s client.SessionSummary, active bool) {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = ch.app.T("nav.untitledSession")
	}
	row := ui.Row(c).Gap(8).Padding(7, 10).Radius(t.RadiusSM).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Focusable().Label(title)
	if row.Clicked() && s.ID != ch.current {
		ch.Open(s.ID)
	}
	if active {
		row.Background(t.AccentSoft)
	} else if row.Hovered() {
		row.Background(t.BgHover)
	}
	if s.PromptPreview != "" {
		row.Tooltip(s.PromptPreview)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Rename…").Chosen() {
			ch.renaming, ch.renameID, ch.renameText = true, s.ID, s.Title
		}
		if m.Item("Delete").Chosen() {
			ch.deleteSession(s.ID)
		}
	})
	row.Children(func() {
		if s.IsRunning {
			kit.Spinner(c).FontSize(12).TextColor(t.Accent)
		}
		txt := ui.Text(c, title).FontSize(t.TextSM).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		if active {
			txt.TextColor(t.Accent).FontWeight(600)
		}
		ui.Text(c, relTime(s.Updated())).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine()
	})
}

func (ch *Chat) deleteSession(id string) {
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.DeleteSession(ctx, id); err != nil {
			ch.app.Toasts.Error("Could not delete the session", err.Error())
			return
		}
		ch.app.Update(func() { ch.removeSession(id) })
	})
}

func (ch *Chat) renameDialog(c *ui.Context) {
	kit.Dialog(c, &ch.renaming, kit.DialogOpts{
		Title: "Rename session",
		Body: func() {
			if kit.Input(c, &ch.renameText, "Title").AutoFocus().Submitted() {
				ch.commitRename()
			}
		},
		Footer: func() {
			if kit.SecondaryButton(c, ch.app.T("common.cancel")).Clicked() {
				ch.renaming = false
			}
			if kit.PrimaryButton(c, "Rename").Clicked() {
				ch.commitRename()
			}
		},
	})
}

func (ch *Chat) commitRename() {
	id, title := ch.renameID, strings.TrimSpace(ch.renameText)
	ch.renaming = false
	if title == "" {
		return
	}
	for i := range ch.sessions {
		if ch.sessions[i].ID == id {
			ch.sessions[i].Title = title
		}
	}
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.RenameSession(ctx, id, title); err != nil {
			ch.app.Toasts.Error("Could not rename the session", err.Error())
		}
	})
}

func (ch *Chat) transcript(c *ui.Context, t *kit.Tokens) {
	cv := ch.conv()
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
			kit.EmptyState(c, kit.IconBot, ch.app.T("chat.empty.title"), ch.app.T("chat.composer.hintIdle"))
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

func (cv *Conversation) keyID() string {
	if cv.ID == "" {
		return fmt.Sprintf("draft-%p", cv)
	}
	return cv.ID
}

func (ch *Chat) item(c *ui.Context, t *kit.Tokens, cv *Conversation, it *Item) {
	switch it.Kind {
	case ItemUser:
		ui.Row(c).Children(func() {
			ui.Spacer(c)
			ui.Text(c, it.Text).Selectable().Padding(10, 14).Radius(t.RadiusLG).Background(t.BgRaised).
				MaxWidth(640).FontSize(t.TextBase)
		})
	case ItemAssistant:
		ui.Column(c).MaxWidth(820).Gap(8).AlignItems(ui.Stretch).Children(func() {
			if it.Thinking != "" {
				ch.thinking(c, t, it)
			}
			for _, tl := range it.Tools {
				ch.tool(c.Key("tool:"+tl.ID), t, tl)
			}
			if it.Text != "" {
				if it.Done {
					markdown.View(c, it.Text, &ch.md)
				} else {
					markdown.Stream(c, it.Text, &ch.md)
				}
			} else if !it.Done && len(it.Tools) == 0 && it.Thinking == "" {
				kit.Spinner(c).TextColor(t.FgFaint)
			}
		})
	case ItemNotice:
		ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
			kit.Icon(c, kit.IconInfo).FontSize(13).TextColor(t.FgFaint).Margin(2, 0, 0, 0)
			ui.Text(c, it.Text).FontSize(t.TextXS).TextColor(t.FgMuted).Selectable().Grow(1).Shrink(1)
		})
	case ItemError:
		ui.Row(c).Gap(8).Padding(10, 12).Radius(t.RadiusMD).Background(t.DangerSoft).AlignItems(ui.Start).Children(func() {
			kit.Icon(c, kit.IconError).FontSize(16).TextColor(t.Danger)
			ui.Text(c, it.Text).FontSize(t.TextSM).TextColor(t.Danger).Selectable().Grow(1).Shrink(1)
		})
	case ItemPermission:
		ch.permission(c, t, cv, it)
	}
}

func (ch *Chat) thinking(c *ui.Context, t *kit.Tokens, it *Item) {
	ui.Column(c).Gap(4).Children(func() {
		head := ui.Row(c).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable()
		head.Children(func() {
			kit.Icon(c, kit.IconThinking).FontSize(14).TextColor(t.FgFaint)
			label := ch.app.T("chat.thought")
			if !it.Done {
				label = ch.app.T("chat.thinking") + "…"
			}
			ui.Text(c, label).FontSize(t.TextXS).TextColor(t.FgMuted)
			chev := kit.IconChevronR
			if it.ThinkingOpen {
				chev = kit.IconChevronDown
			}
			kit.Icon(c, chev).FontSize(12).TextColor(t.FgFaint)
		})
		if head.Clicked() {
			it.ThinkingOpen = !it.ThinkingOpen
		}
		if it.ThinkingOpen {
			ui.Text(c, it.Thinking).FontSize(t.TextXS).TextColor(t.FgMuted).Selectable().
				Padding(0, 0, 0, 20)
		}
	})
}

func (ch *Chat) tool(c *ui.Context, t *kit.Tokens, tl *Tool) {
	ui.Column(c).Radius(t.RadiusSM).Border(1, t.Border).Background(t.BgCard).Children(func() {
		head := ui.Row(c).Gap(8).Padding(6, 10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable()
		head.Children(func() {
			switch tl.Status {
			case "completed":
				kit.Icon(c, kit.IconCheck).FontSize(14).TextColor(t.Success)
			case "failed":
				kit.Icon(c, kit.IconError).FontSize(14).TextColor(t.Danger)
			default:
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
			}
			kit.Icon(c, kit.IconTool).FontSize(13).TextColor(t.FgFaint)
			ui.Text(c, tl.Label()).FontSize(t.TextXS).Font(t.MonoFont).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
		})
		if head.Clicked() {
			tl.Open = !tl.Open
		}
		if tl.Open {
			ui.Column(c).Padding(6, 10, 10, 10).Gap(6).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				if tl.Input != "" {
					ui.Text(c, clip(tl.Input, 4000)).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted).Selectable()
				}
				if tl.Output != "" {
					out := ui.Text(c, clip(tl.Output, 8000)).FontSize(t.TextXS).Font(t.MonoFont).Selectable()
					if tl.IsError {
						out.TextColor(t.Danger)
					}
				}
			})
		}
	})
}

func (ch *Chat) permission(c *ui.Context, t *kit.Tokens, cv *Conversation, it *Item) {
	p := it.Permission
	kit.Card(c, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, kit.IconWarning).TextColor(t.Warning)
			ui.Text(c, ch.app.T("chat.permission.title")).FontWeight(600).FontSize(t.TextSM)
			kit.Badge(c, p.ToolName, kit.ToneWarning)
		})
		if p.Description != "" {
			ui.Text(c, p.Description).FontSize(t.TextSM).Selectable()
		}
		if p.Path != "" {
			ui.Text(c, p.Path).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted)
		}
		if it.Answer != "" {
			ui.Text(c, it.Answer).FontSize(t.TextXS).TextColor(t.FgMuted)
			return
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			answer := func(action, label string) {
				it.Answer = label
				id, sid := p.ID, p.SessionID
				if sid == "" {
					sid = cv.ID
				}
				ch.app.Go(func(ctx context.Context) {
					if err := ch.app.Client.RespondPermission(ctx, sid, id, action); err != nil {
						ch.app.Toasts.Error("Could not answer the permission request", err.Error())
					}
				})
			}
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: ch.app.T("chat.permission.deny")}).Clicked() {
				answer("deny", ch.app.T("chat.permission.deny"))
			}
			if !p.NeverAutoApprove {
				if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Label: ch.app.T("chat.permission.allowSession")}).Clicked() {
					answer("allow_session", ch.app.T("chat.permission.allowSession"))
				}
			}
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Label: ch.app.T("chat.permission.allow")}).Clicked() {
				answer("allow", ch.app.T("chat.permission.allow"))
			}
		})
	}).MaxWidth(640)
}

func (ch *Chat) composer(c *ui.Context, t *kit.Tokens) {
	cv := ch.conv()
	ui.Column(c).Padding(8, 24, 16, 24).Gap(6).Children(func() {
		ui.Column(c).Radius(t.RadiusLG).Border(1, t.BorderStrong).Background(t.BgInput).Padding(10, 12).Gap(8).
			Children(func() {
				placeholder := ch.app.T("chat.composer.placeholder")
				if cv.Running {
					placeholder = ch.app.T("chat.composer.placeholderRunning")
				}
				ui.TextAreaBase(c.Key("composer"), &ch.draft).Lines(1, 10).Placeholder(placeholder).
					FontSize(t.TextBase).AutoFocus().
					OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEnter && k.Mods == 0 {
							text := k.Text()
							if strings.TrimSpace(text) == "" {
								return true
							}
							k.SetText("")
							ch.Send(text)
							return true
						}
						return false // Shift+Enter breaks the line
					})
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					hint := ch.app.T("chat.composer.hintIdle")
					if cv.Running {
						hint = ch.app.T("chat.composer.hintRunning")
					}
					ui.Text(c, hint).FontSize(t.TextXS).TextColor(t.FgFaint).Grow(1).Shrink(1).SingleLine().Ellipsis("…")
					if cv.Running {
						if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Icon: kit.IconStop,
							Tooltip: ch.app.T("chat.composer.stop")}).Clicked() {
							ch.Stop()
						}
					}
					send := kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Icon: kit.IconSend,
						Tooltip: ch.app.T("chat.composer.send"), Disabled: strings.TrimSpace(ch.draft) == ""})
					if send.Clicked() {
						text := ch.draft
						ch.draft = ""
						ch.Send(text)
					}
				})
			})
	})
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n… (truncated)"
}

// relTime formats a time as a short age: "now", "5m", "3h", "2d", or a date.
func relTime(tm time.Time) string {
	d := time.Since(tm)
	switch {
	case tm.IsZero():
		return ""
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	return tm.Format("Jan 2")
}
