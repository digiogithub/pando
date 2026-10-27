package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

const maxChars = 8000

// menuKind is the completion popup the composer shows.
type menuKind int

const (
	menuNone menuKind = iota
	menuSlash
	menuMention
)

type menuItem struct {
	Label, Desc string
	// Text replaces the draft (slash) or the @token (mention).
	Text string
}

// composerState is the composer's completion and history state.
type composerState struct {
	kind  menuKind
	items []menuItem
	idx   int
	// dismissedFor is the draft the popup was dismissed with Escape for.
	dismissedFor string

	commands     []client.SlashCommand
	commandsLoad bool

	mentionQuery   string
	mentionHits    []client.FileHit
	mentionPending string
	mentionCancel  context.CancelFunc

	history []string
	histIdx int // -1: not browsing
	histOrg string
}

// mentionToken returns the "@query" at the end of the draft and its start
// offset (-1 when the draft does not end in a mention).
func mentionToken(draft string) (query string, start int) {
	i := strings.LastIndex(draft, "@")
	if i < 0 || strings.ContainsAny(draft[i+1:], " \t\n") {
		return "", -1
	}
	if i > 0 && !strings.ContainsAny(draft[i-1:i], " \t\n") {
		return "", -1 // an e-mail address, not a mention
	}
	return draft[i+1:], i
}

// updateMenu recomputes the popup from the draft (called every Build).
func (ch *Chat) updateMenu() {
	cs := &ch.comp
	cs.kind, cs.items = menuNone, nil
	d := ch.draft
	if d == cs.dismissedFor {
		return
	}
	switch {
	case strings.HasPrefix(d, "/") && !strings.ContainsAny(d, " \n"):
		prefix := strings.ToLower(d[1:])
		for _, cmd := range cs.commands {
			if strings.HasPrefix(strings.ToLower(cmd.Name), prefix) {
				text := "/" + cmd.Name
				if cmd.AcceptsArgs {
					text += " "
				}
				cs.items = append(cs.items, menuItem{Label: "/" + cmd.Name, Desc: cmd.Description, Text: text})
			}
		}
		cs.kind = menuSlash
	default:
		q, start := mentionToken(d)
		if start < 0 || ch.app.Client == nil {
			return
		}
		cs.kind = menuMention
		if q != cs.mentionQuery {
			cs.mentionQuery = q
			ch.searchFiles(q)
		}
		for i, h := range cs.mentionHits {
			if i >= 8 {
				break
			}
			cs.items = append(cs.items, menuItem{Label: h.Name, Desc: h.Path, Text: h.Path})
		}
	}
	if len(cs.items) == 0 {
		cs.kind = menuNone
	}
	cs.idx = min(cs.idx, max(len(cs.items)-1, 0))
}

// searchFiles queries /files/search for a mention, cancelling the previous
// query.
func (ch *Chat) searchFiles(q string) {
	cs := &ch.comp
	if cs.mentionCancel != nil {
		cs.mentionCancel()
	}
	if q == "" {
		cs.mentionHits = nil
		return
	}
	ctx, cancel := context.WithCancel(ch.app.Ctx)
	cs.mentionCancel = cancel
	ch.app.Go(func(context.Context) {
		select { // debounce
		case <-ctx.Done():
			return
		case <-time.After(120 * time.Millisecond):
		}
		hits, err := ch.app.Client.SearchFiles(ctx, q)
		if err != nil || ctx.Err() != nil {
			return
		}
		sort.SliceStable(hits, func(i, j int) bool { return len(hits[i].Path) < len(hits[j].Path) })
		ch.app.Update(func() {
			if cs.mentionQuery == q {
				cs.mentionHits = hits
			}
		})
	})
}

// applyMenu inserts the selected completion into the draft text.
func (ch *Chat) applyMenu(text string) string {
	cs := &ch.comp
	if cs.idx >= len(cs.items) {
		return text
	}
	it := cs.items[cs.idx]
	var out string
	if cs.kind == menuSlash {
		out = it.Text
	} else {
		_, start := mentionToken(text)
		if start < 0 {
			return text
		}
		out = text[:start] + "@" + it.Text + " "
	}
	cs.dismissedFor = out
	return out
}

// loadCommands fetches the slash commands once.
func (ch *Chat) loadCommands() {
	cs := &ch.comp
	if cs.commandsLoad || ch.app.Client == nil {
		return
	}
	cs.commandsLoad = true
	ch.app.Go(func(ctx context.Context) {
		cmds, err := ch.app.Client.Commands(ctx)
		if err == nil {
			ch.app.Update(func() { ch.comp.commands = cmds })
		}
	})
}

// pushHistory remembers a sent prompt for Up/Down recall.
func (ch *Chat) pushHistory(text string) {
	cs := &ch.comp
	if n := len(cs.history); n == 0 || cs.history[n-1] != text {
		cs.history = append(cs.history, text)
	}
	cs.histIdx = -1
}

// onKey handles the composer keys: send, completion popup navigation and
// prompt history.
func (ch *Chat) onKey(k ui.TextKey) bool {
	cs := &ch.comp
	text := k.Text()
	if cs.kind != menuNone && len(cs.items) > 0 && k.Mods == 0 {
		switch k.Key {
		case ui.KeyDown:
			cs.idx = min(cs.idx+1, len(cs.items)-1)
			return true
		case ui.KeyUp:
			cs.idx = max(cs.idx-1, 0)
			return true
		case ui.KeyTab:
			k.SetText(ch.applyMenu(text))
			return true
		case ui.KeyEnter:
			// A command typed in full is sent, not re-completed.
			if cs.kind == menuSlash && strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(cs.items[cs.idx].Text)) {
				break
			}
			k.SetText(ch.applyMenu(text))
			return true
		case ui.KeyEscape:
			cs.dismissedFor = text
			return true
		}
	}
	switch {
	case k.Key == ui.KeyEnter && k.Mods == 0:
		if strings.TrimSpace(text) == "" || len([]rune(text)) > maxChars {
			return true
		}
		k.SetText("")
		cs.histIdx = -1
		ch.Send(text)
		return true
	case k.Key == ui.KeyUp && k.Mods == 0 && (strings.TrimSpace(text) == "" || cs.histIdx >= 0) && len(cs.history) > 0:
		if cs.histIdx < 0 {
			cs.histIdx, cs.histOrg = len(cs.history), text
		}
		cs.histIdx = max(cs.histIdx-1, 0)
		k.SetText(cs.history[cs.histIdx])
		return true
	case k.Key == ui.KeyDown && k.Mods == 0 && cs.histIdx >= 0:
		cs.histIdx++
		if cs.histIdx >= len(cs.history) {
			cs.histIdx = -1
			k.SetText(cs.histOrg)
		} else {
			k.SetText(cs.history[cs.histIdx])
		}
		return true
	}
	return false // Shift+Enter breaks the line
}

func (ch *Chat) composer(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	ch.loadCommands()
	ch.updateMenu()
	ui.Column(c).Padding(4, 24, 14, 24).Gap(6).AlignItems(ui.Stretch).Children(func() {
		if cv.BusyElsewhere && !cv.Streaming {
			muted(c, t, kit.IconInfo, ch.app.T("chat.busyElsewhere.notice"), false)
		}
		for i, q := range cv.Queued {
			ui.Row(c.Key(fmt.Sprint("q", i))).Gap(6).AlignItems(ui.Start).Children(func() {
				kit.Icon(c, kit.IconClock).FontSize(12).TextColor(t.FgFaint).Margin(2, 0, 0, 0)
				ui.Text(c, q).FontSize(t.TextXS).TextColor(t.FgMuted).Grow(1).Shrink(1)
				kit.Badge(c, ch.app.T("chat.queued"), kit.ToneNeutral)
			})
		}
		ch.completionMenu(c, t)
		ui.Column(c).Radius(t.RadiusLG).Border(1, t.BorderStrong).Background(t.BgInput).Padding(10, 12).Gap(8).
			Children(func() {
				placeholder := ch.app.T("chat.composer.placeholder")
				if cv.Running {
					placeholder = ch.app.T("chat.composer.placeholderRunning")
				}
				ui.TextAreaBase(c.Key("composer"), &ch.draft).Lines(1, 9).Placeholder(placeholder).
					FontSize(t.TextBase).AutoFocus().OnTextKey(ch.onKey)
				ch.composerActions(c, t, cv)
			})
		ch.composerMeta(c, t, cv)
	})
}

func (ch *Chat) completionMenu(c *ui.Context, t *kit.Tokens) {
	cs := &ch.comp
	if cs.kind == menuNone {
		return
	}
	title := ch.app.T("chat.slashCommands")
	if cs.kind == menuMention {
		title = "@"
	}
	ui.Column(c).Radius(t.RadiusMD).Border(1, t.Border).Background(t.BgOverlay).Padding(4).MaxHeight(260).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.12)).Children(func() {
		ui.Text(c, strings.ToUpper(title)).FontSize(10).FontWeight(600).LetterSpacing(0.5).TextColor(t.FgFaint).Padding(4, 8)
		ui.Scroll(c).Children(func() {
			for i, it := range cs.items {
				row := ui.Row(c.Key(fmt.Sprint("mi", i))).Gap(10).Padding(5, 8).Radius(t.RadiusSM).AlignItems(ui.Center).
					Cursor(ui.CursorPointer).Label(it.Label)
				if i == cs.idx {
					row.Background(t.AccentSoft)
				} else if row.Hovered() {
					row.Background(t.BgHover)
				}
				if row.Clicked() {
					cs.idx = i
					ch.draft = ch.applyMenu(ch.draft)
				}
				row.Children(func() {
					ui.Text(c, it.Label).FontSize(t.TextSM).Font(t.MonoFont).FontWeight(600).SingleLine()
					ui.Text(c, it.Desc).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
				})
			}
		})
	})
}

func (ch *Chat) composerActions(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	hasText := strings.TrimSpace(ch.draft) != ""
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		hint := ch.app.T("chat.composer.hintIdle")
		switch {
		case cv.Running:
			hint = ch.app.T("chat.composer.hintRunning")
		case cv.Goal != nil && cv.Goal.Status == "running":
			hint = ch.app.T("chat.composer.hintGoal")
		}
		ui.Text(c, hint).FontSize(t.TextXS).TextColor(t.FgFaint).Grow(1).Shrink(1).SingleLine().Ellipsis("…")
		ch.personaChip(c, t)
		ch.modelChip(c, t, cv)
		if cv.ID != "" {
			on := cv.AutoApprove
			sw := kit.Switch(c.Key("autoapprove"), &on, "").Tooltip(ch.app.T("common.toggleAutoApprove"))
			if on != cv.AutoApprove {
				ch.setAutoApprove(cv, on)
			}
			_ = sw
			ui.Text(c, "Auto").FontSize(t.TextXS).TextColor(t.FgMuted)
		}
		if cv.Running {
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Secondary, Size: kit.SizeSM, Icon: kit.IconStop,
				Tooltip: ch.app.T("chat.composer.stop")}).Clicked() {
				ch.Stop()
			}
		}
		if !cv.Running || hasText {
			tip := ch.app.T("chat.composer.send")
			if cv.Running {
				tip = ch.app.T("chat.composer.queue")
			}
			send := kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Size: kit.SizeSM, Icon: kit.IconSend, Tooltip: tip,
				Disabled: !hasText || (cv.BusyElsewhere && !cv.Streaming) || len([]rune(ch.draft)) > maxChars})
			if send.Clicked() {
				text := ch.draft
				ch.draft = ""
				ch.Send(text)
			}
		}
	})
}

func (ch *Chat) composerMeta(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	n := len([]rune(ch.draft))
	total, window := ch.sessionTokens(cv)
	pct := contextPct(total, window)
	if ch.cwd == "" && n < maxChars*9/10 && total == 0 {
		return
	}
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		if ch.cwd != "" {
			kit.Icon(c, kit.IconFolder).FontSize(12).TextColor(t.FgFaint)
			ui.Text(c, ch.cwd).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine().Ellipsis("…").Shrink(1).Tooltip(ch.cwd)
		}
		ui.Spacer(c)
		if n >= maxChars*9/10 {
			ui.Text(c, ch.app.T("chat.composer.chars", "count", n, "max", maxChars)).FontSize(t.TextXS).TextColor(t.Warning)
		}
		if total > 0 {
			kit.Icon(c, icoGauge).FontSize(12).TextColor(t.FgFaint)
			if pct >= 0 {
				ui.Row(c).Width(48).Height(4).Radius(2).Background(t.BgRaised).Children(func() {
					ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(meterColor(t, pct))
				})
				ui.Text(c, ch.app.T("chat.composer.context", "pct", fmt.Sprintf("%.0f", pct))).FontSize(t.TextXS).TextColor(t.FgFaint).
					Tooltip(formatCount(total) + " / " + formatCount(window))
			} else {
				ui.Text(c, ch.app.T("chat.composer.tokens", "formatted", formatCount(total))).FontSize(t.TextXS).TextColor(t.FgFaint)
			}
		}
	})
}

// sessionTokens is the context usage of the session: the live token_usage
// of the run, else the totals the sessions list carries.
func (ch *Chat) sessionTokens(cv *Conversation) (total, window int64) {
	window = cv.ContextWindow
	if cv.Usage != nil && (cv.Running || cv.Usage.PromptTokens > 0) {
		return cv.Usage.PromptTokens + cv.Usage.CompletionTokens, max(window, cv.Usage.ContextWindow)
	}
	if s, ok := ch.session(cv.ID); ok {
		return s.PromptTokens + s.CompletionTokens, window
	}
	return 0, window
}

func (ch *Chat) setAutoApprove(cv *Conversation, on bool) {
	cv.AutoApprove = on
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.SetAutoApprove(ctx, id, on); err != nil {
			ch.app.Update(func() { cv.AutoApprove = !on })
			ch.app.Toasts.Error("Could not change auto-approve", err.Error())
		}
	})
}
