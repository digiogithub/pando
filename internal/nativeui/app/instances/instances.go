// Package instances is the instances area: the Pando instances running on
// this machine grouped by project path, and for the selected one its
// sessions with the conversation history, the live event stream
// (/instances/{id}/sessions/{sid}/stream), a message box and cancel. It
// mirrors web-ui/src/components/instances.
package instances

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

func init() {
	app.Register(app.Area{
		ID: "instances", Path: "/instances", TitleKey: "nav.instances",
		Icon: kit.IconInstances, Section: app.SectionServices, Order: 0,
		New: func(a *app.Context) app.View { return New(a) },
	})
}

const (
	pageSize  = 100
	maxEvents = 200
)

// noiseTopics carry no conversation value (heartbeats fire every 5 s).
var noiseTopics = map[string]bool{"instance.heartbeat": true, "instance.ping": true}

// Instances is the area's view.
type Instances struct {
	app *app.Context

	started bool
	loading bool
	loaded  bool
	err     string
	list    []client.InstanceInfo

	selected string // instance id
	instList ui.ListState

	// Remote sessions of the selected instance.
	sessions    []client.RemoteSession
	sessTotal   int
	sessMore    bool
	sessLoading bool
	sessErr     string
	sessGen     int
	sessList    ui.ListState

	// Selected session.
	sel      string
	history  []client.RemoteMessage
	histLoad bool
	histErr  string
	events   []streamRow
	seq      int
	live     bool
	gen      int
	cancelFn context.CancelFunc
	convList ui.ListState
	batch    *app.Batch[streamMsg]

	showInput  bool
	draft      string
	sending    bool
	cancelling bool
}

type streamRow struct {
	seq     int
	topic   string
	content string
	raw     string
}

type streamMsg struct {
	gen    int
	opened bool
	ev     client.InstanceEvent
}

// New returns the instances view.
func New(a *app.Context) *Instances {
	in := &Instances{app: a}
	in.convList.FollowEnd = true
	in.convList.FollowAnimation = &ui.Smooth
	in.convList.Key = func(i int) any { return in.convKey(i) }
	in.sessList.Key = func(i int) any {
		if i < len(in.sessions) {
			return in.sessions[i].ID
		}
		return i
	}
	in.batch = app.NewBatch(a.Update, in.applyStream)
	return in
}

// Close stops the stream.
func (in *Instances) Close() {
	if in.cancelFn != nil {
		in.cancelFn()
	}
}

// HeaderActions adds the refresh button.
func (in *Instances) HeaderActions(c *ui.Context) {
	if kit.IconButton(c, kit.IconRefresh, "Refresh").Clicked() {
		in.Refresh()
	}
}

// Refresh reloads the instance list.
func (in *Instances) Refresh() {
	if in.loading {
		return
	}
	in.loading = true
	in.app.Go(func(ctx context.Context) {
		l, err := in.app.Client.Instances(ctx)
		in.app.Update(func() {
			in.loading, in.loaded = false, true
			if err != nil {
				in.err = err.Error()
				return
			}
			in.err = ""
			in.list = l
		})
	})
}

// Select shows the sessions of an instance.
func (in *Instances) Select(id string) {
	if id == in.selected {
		return
	}
	in.selected = id
	in.sessions, in.sessTotal, in.sessMore, in.sessErr = nil, 0, false, ""
	in.selectSession("")
	in.sessGen++
	in.loadSessions(0)
}

func (in *Instances) loadSessions(offset int) {
	gen, id := in.sessGen, in.selected
	in.sessLoading = true
	in.app.Go(func(ctx context.Context) {
		pg, err := in.app.Client.RemoteSessions(ctx, id, pageSize, offset)
		in.app.Update(func() {
			if gen != in.sessGen {
				return
			}
			in.sessLoading = false
			if err != nil {
				in.sessErr = err.Error()
				return
			}
			known := map[string]bool{}
			for _, s := range in.sessions {
				known[s.ID] = true
			}
			for _, s := range pg.Sessions {
				if !known[s.ID] {
					in.sessions = append(in.sessions, s)
				}
			}
			in.sessTotal = max(pg.Total, len(in.sessions))
			in.sessMore = pg.HasMore
		})
	})
}

// selectSession loads the history and follows the live stream of a session
// ("" clears the selection).
func (in *Instances) selectSession(sid string) {
	if in.cancelFn != nil {
		in.cancelFn()
		in.cancelFn = nil
	}
	in.gen++
	gen := in.gen
	in.sel = sid
	in.history, in.events, in.histErr, in.live = nil, nil, "", false
	in.showInput, in.draft = false, ""
	in.convList.ScrollToEnd()
	if sid == "" {
		return
	}
	inst := in.selected
	in.histLoad = true
	ctx, cancel := context.WithCancel(in.app.Ctx)
	in.cancelFn = cancel
	in.app.Go(func(context.Context) {
		msgs, err := in.app.Client.RemoteMessages(ctx, inst, sid)
		in.app.Update(func() {
			if gen != in.gen {
				return
			}
			in.histLoad = false
			if err != nil {
				in.histErr = err.Error()
				return
			}
			in.history = msgs
		})
	})
	in.app.Go(func(context.Context) {
		_ = in.app.Client.FollowRemoteSession(ctx, inst, sid, func(ev client.InstanceEvent) {
			in.batch.Push(streamMsg{gen: gen, ev: ev})
		})
		in.batch.Push(streamMsg{gen: gen, opened: false, ev: client.InstanceEvent{Topic: "\x00closed"}})
	})
}

func (in *Instances) applyStream(msgs []streamMsg) {
	for _, m := range msgs {
		if m.gen != in.gen {
			continue
		}
		if m.ev.Topic == "\x00closed" {
			in.live = false
			continue
		}
		in.live = true // any frame, heartbeats included, proves the stream is alive
		if noiseTopics[m.ev.Topic] {
			continue
		}
		in.seq++
		row := streamRow{seq: in.seq, topic: m.ev.Topic, raw: string(m.ev.Payload)}
		var p map[string]any
		if json.Unmarshal(m.ev.Payload, &p) == nil {
			if s, ok := p["token"].(string); ok {
				row.content = s
			} else if s, ok := p["content"].(string); ok {
				row.content = s
			}
		}
		if row.content == "" {
			row.content = row.raw
		}
		in.events = append(in.events, row)
	}
	if len(in.events) > maxEvents {
		in.events = append([]streamRow(nil), in.events[len(in.events)-maxEvents:]...)
	}
}

func (in *Instances) send() {
	text := strings.TrimSpace(in.draft)
	if text == "" || in.sel == "" {
		return
	}
	inst, sid := in.selected, in.sel
	in.sending = true
	in.app.Go(func(ctx context.Context) {
		err := in.app.Client.SendRemoteMessage(ctx, inst, sid, text)
		in.app.Update(func() {
			in.sending = false
			if err != nil {
				in.app.Toasts.Error("Could not send the message", err.Error())
				return
			}
			in.draft, in.showInput = "", false
		})
	})
}

func (in *Instances) cancel() {
	inst, sid := in.selected, in.sel
	in.cancelling = true
	in.app.Go(func(ctx context.Context) {
		err := in.app.Client.CancelRemote(ctx, inst, sid)
		in.app.Update(func() {
			in.cancelling = false
			if err != nil {
				in.app.Toasts.Error("Could not cancel the run", err.Error())
			}
		})
	})
}

func (in *Instances) convLen() int {
	n := len(in.history) + len(in.events)
	if len(in.history) > 0 {
		n++
	}
	return n
}

func (in *Instances) convKey(i int) any {
	switch {
	case i < len(in.history):
		return "h" + in.history[i].ID
	case len(in.history) > 0 && i == len(in.history):
		return "divider"
	}
	j := i - len(in.history)
	if len(in.history) > 0 {
		j--
	}
	if j >= 0 && j < len(in.events) {
		return in.events[j].seq
	}
	return i
}

// Build draws the view.
func (in *Instances) Build(c *ui.Context, _ *ui.Route) {
	if !in.started {
		in.started = true
		in.Refresh()
	}
	t := kit.Current()
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		in.instanceColumn(c, t)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			inst := in.find(in.selected)
			if inst == nil {
				kit.EmptyState(c, kit.IconInstances, "Select an instance to view its sessions", "")
				return
			}
			in.remote(c, t)
		})
	})
}

func (in *Instances) find(id string) *client.InstanceInfo {
	for i := range in.list {
		if in.list[i].InstanceID == id {
			return &in.list[i]
		}
	}
	return nil
}

func (in *Instances) instanceColumn(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Width(290).Shrink(0).AlignItems(ui.Stretch).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).Padding(10, 14).Gap(6).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, kit.IconInstances).FontSize(14).TextColor(t.FgMuted)
			ui.Text(c, fmt.Sprintf("Instances (%d)", len(in.list))).FontSize(t.TextSM).FontWeight(600).Grow(1)
			if in.loading {
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
			}
		})
		if in.err != "" {
			ui.Text(c, in.err).FontSize(t.TextXS).TextColor(t.Danger).Padding(4, 14)
		}
		if in.loaded && len(in.list) == 0 {
			kit.EmptyState(c, kit.IconInstances, "No running instances found.", "Start another Pando instance to see it listed here.")
			return
		}
		groups := map[string][]client.InstanceInfo{}
		var paths []string
		for _, i := range in.list {
			if _, ok := groups[i.Path]; !ok {
				paths = append(paths, i.Path)
			}
			groups[i.Path] = append(groups[i.Path], i)
		}
		sort.Strings(paths)
		ui.Scroll(c).Grow(1).Padding(0, 6, 8, 6).Gap(2).Children(func() {
			for _, p := range paths {
				ui.Text(c, app.ShortenPath(p)).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgFaint).
					SingleLine().Ellipsis("…").Padding(8, 8, 2, 8).Tooltip(p)
				for _, i := range groups[p] {
					in.instanceCard(c, t, i)
				}
			}
		})
	})
}

var modeTone = map[string]kit.Tone{"tui": kit.ToneAccent, "webui": kit.ToneInfo, "desktop": kit.ToneAccent, "acp": kit.ToneSuccess}

func (in *Instances) instanceCard(c *ui.Context, t *kit.Tokens, i client.InstanceInfo) {
	active := i.InstanceID == in.selected
	row := ui.Column(c.Key("inst:"+i.InstanceID)).Gap(4).Padding(8, 10).Radius(t.RadiusSM).Cursor(ui.CursorPointer).
		Focusable().Label(app.ShortenPath(i.Path))
	if row.Clicked() {
		in.Select(i.InstanceID)
	}
	if active {
		row.Background(t.AccentSoft)
	} else if row.Hovered() {
		row.Background(t.BgHover)
	}
	row.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ic := kit.Icon(c, kit.IconInstances).FontSize(13)
			if active {
				ic.TextColor(t.Accent)
			} else {
				ic.TextColor(t.FgMuted)
			}
			ui.Text(c, app.ShortenPath(i.Path)).FontSize(t.TextXS).Font(t.MonoFont).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
			if i.IsPrimary {
				kit.Badge(c, "PRIMARY", kit.ToneWarning)
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 0, 21).Children(func() {
			kit.Badge(c, strings.ToUpper(i.Mode), modeTone[i.Mode])
			ui.Text(c, fmt.Sprintf("PID %d", i.PID)).FontSize(t.TextXS).TextColor(t.FgMuted)
			id := i.InstanceID
			if len(id) > 8 {
				id = id[:8]
			}
			ui.Text(c, id).FontSize(10).TextColor(t.FgFaint).Grow(1).TextAlign(ui.End)
		})
	})
}

func (in *Instances) remote(c *ui.Context, t *kit.Tokens) {
	ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
		in.sessionColumn(c, t)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Stretch).Children(func() {
			if in.sel == "" {
				kit.EmptyState(c, kit.IconChat, "Select a session to view its live stream", "")
				return
			}
			in.streamPanel(c, t)
		})
	})
}

func (in *Instances) sessionColumn(c *ui.Context, t *kit.Tokens) {
	ui.Column(c).Width(240).Shrink(0).AlignItems(ui.Stretch).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		head := fmt.Sprintf("Sessions (%d", len(in.sessions))
		if in.sessTotal > len(in.sessions) {
			head += fmt.Sprintf("/%d", in.sessTotal)
		}
		head += ")"
		ui.Row(c).Padding(10, 14).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, head).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgFaint).Grow(1)
			if in.sessLoading {
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
			}
		})
		if in.sessErr != "" {
			ui.Text(c, in.sessErr).FontSize(t.TextXS).TextColor(t.Danger).Padding(4, 14)
		}
		n := len(in.sessions)
		ui.List(c.Key("rsess:"+in.selected), &in.sessList, n, func(i int) {
			s := in.sessions[i]
			title := s.Title
			if title == "" {
				title = "Untitled"
			}
			row := ui.Column(c).Gap(1).Padding(6, 10).Radius(t.RadiusSM).Cursor(ui.CursorPointer).Focusable().Label(title)
			if row.Clicked() && s.ID != in.sel {
				in.selectSession(s.ID)
			}
			if s.ID == in.sel {
				row.Background(t.AccentSoft)
			} else if row.Hovered() {
				row.Background(t.BgHover)
			}
			row.Children(func() {
				ui.Text(c, title).FontSize(t.TextXS).SingleLine().Ellipsis("…")
				ui.Text(c, fmt.Sprintf("%d msgs · %s", s.MessageCount, fmtTime(s.UpdatedAt))).FontSize(10).TextColor(t.FgMuted)
			})
		}).Grow(1).Padding(0, 6, 8, 6).Gap(1).Children(func() {
			if n == 0 && !in.sessLoading {
				ui.Text(c, "No sessions found").FontSize(t.TextSM).TextColor(t.FgMuted).Padding(12)
			}
		})
		if in.sessMore {
			ui.Row(c).Padding(8).Children(func() {
				label := fmt.Sprintf("Load more (%d/%d)", len(in.sessions), in.sessTotal)
				if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Label: label, Disabled: in.sessLoading}).Clicked() {
					in.loadSessions(len(in.sessions))
				}
			})
		}
	})
}

func fmtTime(s string) string {
	if tm, err := time.Parse(time.RFC3339, s); err == nil {
		return tm.Local().Format("Jan 2 15:04")
	}
	return s
}

func (in *Instances) streamPanel(c *ui.Context, t *kit.Tokens) {
	title := "Untitled session"
	for _, s := range in.sessions {
		if s.ID == in.sel && s.Title != "" {
			title = s.Title
		}
	}
	ui.Row(c).Padding(10, 16).Gap(8).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		dot := t.FgFaint
		state := "connecting…"
		if in.live {
			dot, state = t.Success, "live"
		}
		ui.Box(c).Width(8).Height(8).Radius(4).Background(dot)
		ui.Text(c, title).FontSize(t.TextSM).FontWeight(600).SingleLine().Ellipsis("…").Shrink(1)
		ui.Text(c, state).FontSize(t.TextXS).TextColor(t.FgFaint).Grow(1)
		if kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Icon: kit.IconSend, Label: "Send Message"}).Clicked() {
			in.showInput = !in.showInput
		}
		if kit.Button(c, kit.ButtonOpts{Variant: kit.Danger, Size: kit.SizeSM, Icon: kit.IconStop, Label: "Cancel", Disabled: in.cancelling}).Clicked() {
			in.cancel()
		}
	})
	if in.showInput {
		ui.Row(c).Padding(10, 16).Gap(8).AlignItems(ui.End).Background(t.BgShell).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			ui.TextAreaBase(c.Key("remote-draft"), &in.draft).Lines(3, 8).
				Placeholder("Type a message… (Enter to send, Shift+Enter for newline)").
				FontSize(t.TextSM).Grow(1).Padding(8).Radius(t.RadiusSM).Background(t.BgInput).Border(1, t.BorderStrong).AutoFocus().
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key == ui.KeyEnter && k.Mods == 0 {
						in.draft = k.Text()
						in.send()
						return true
					}
					return false
				})
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Label: "Send", Disabled: in.sending || strings.TrimSpace(in.draft) == ""}).Clicked() {
				in.send()
			}
		})
	}
	ui.List(c.Key("remote-conv:"+in.sel), &in.convList, in.convLen(), func(i int) {
		in.convRow(c, t, i)
	}).Grow(1).Padding(4, 0).Children(func() {
		switch {
		case in.histLoad:
			ui.Row(c).Margin(ui.Auto).Gap(8).Children(func() {
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
				ui.Text(c, "Loading conversation…").FontSize(t.TextSM).TextColor(t.FgFaint)
			})
		case in.histErr != "":
			ui.Text(c, "Could not load conversation: "+in.histErr).FontSize(t.TextXS).TextColor(t.Danger).Padding(12, 16)
		case in.convLen() == 0:
			ui.Text(c, "Waiting for events…").FontSize(t.TextSM).TextColor(t.FgFaint).Padding(24).TextAlign(ui.Center)
		}
	})
}

func (in *Instances) convRow(c *ui.Context, t *kit.Tokens, i int) {
	border := func(e ui.Element) ui.Element { return e.BorderWidth(0, 0, 1, 0).BorderColor(t.Border) }
	switch {
	case i < len(in.history):
		m := in.history[i]
		col, role := t.Success, m.Role
		if m.Role == "user" {
			col = t.Accent
		}
		border(ui.Column(c).Gap(2).Padding(8, 16)).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, strings.ToUpper(role)).FontSize(10).FontWeight(700).TextColor(col)
				ui.Text(c, fmtTime(m.CreatedAt)).FontSize(10).TextColor(t.FgFaint)
			})
			ui.Text(c, m.Content).FontSize(t.TextSM).Selectable()
		})
		return
	case i == len(in.history) && len(in.history) > 0:
		ui.Text(c, "Live stream").FontSize(t.TextXS).FontWeight(600).TextColor(t.FgFaint).Padding(8, 16, 4, 16)
		return
	}
	j := i - len(in.history)
	if len(in.history) > 0 {
		j--
	}
	if j < 0 || j >= len(in.events) {
		return
	}
	ev := in.events[j]
	border(ui.Row(c).Gap(8).Padding(2, 16).AlignItems(ui.Start)).Children(func() {
		ui.Text(c, ev.topic).FontSize(10).FontWeight(600).Font(t.MonoFont).TextColor(topicColor(t, ev.topic)).Width(130).Shrink(0)
		tx := ui.Text(c, ev.content).FontSize(t.TextXS).Font(t.MonoFont).Grow(1).Shrink(1).MinWidth(0).Selectable()
		if ev.content == ev.raw {
			tx.TextColor(t.FgFaint)
		}
	})
}

func topicColor(t *kit.Tokens, topic string) ui.Color {
	prefix, _, _ := strings.Cut(topic, ".")
	switch prefix {
	case "llm":
		return t.Accent
	case "tool":
		return t.Warning
	case "session":
		return t.Info
	case "message":
		return t.Success
	case "instance":
		return t.FgMuted
	}
	return t.FgFaint
}
