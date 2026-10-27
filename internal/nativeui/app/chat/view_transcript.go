package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

func copyText(s string) bool { return mygo.Clipboard.Write(transfer.TextData(s)) == nil }

// inlineDiff caches the diff state of one edit/write tool card.
type inlineDiff struct {
	state    *editor.DiffState
	old, new string
}

func (ch *Chat) item(c *ui.Context, t *kit.Tokens, cv *Conversation, it *Item) {
	switch it.Kind {
	case ItemUser:
		ch.userRow(c, t, it)
	case ItemAssistant:
		ch.assistantTurn(c, t, cv, it)
	case ItemNotice:
		muted(c, t, kit.IconInfo, it.Text, false)
	case ItemRouting:
		ch.routingRow(c, t, it)
	case ItemContextFilter:
		ch.contextFilterRow(c, t, it)
	case ItemPersona:
		ch.personaRow(c, t, it)
	case ItemError:
		ui.Row(c).Gap(8).Padding(10, 12).Radius(t.RadiusMD).Background(t.DangerSoft).AlignItems(ui.Start).Children(func() {
			kit.Icon(c, kit.IconError).FontSize(16).TextColor(t.Danger)
			ui.Text(c, it.Text).FontSize(t.TextSM).TextColor(t.Danger).Selectable().Grow(1).Shrink(1)
		})
	}
}

// muted is the compact one-line notice row (routing, notices).
func muted(c *ui.Context, t *kit.Tokens, icon, text string, warn bool) {
	col := t.FgMuted
	if warn {
		col = t.Warning
	}
	ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
		kit.Icon(c, icon).FontSize(12).TextColor(col).Margin(2, 0, 0, 0)
		ui.Text(c, text).FontSize(t.TextXS).TextColor(col).Selectable().Grow(1).Shrink(1)
	})
}

func (ch *Chat) userRow(c *ui.Context, t *kit.Tokens, it *Item) {
	ui.Column(c).AlignItems(ui.End).Gap(4).Children(func() {
		for i, src := range it.Images {
			if bmp := ch.bitmap(src); bmp != nil {
				ui.Image(c.Key(fmt.Sprintf("uimg%d", i)), bmp).MaxWidth(320).MaxHeight(240).Fit(ui.Contain).Radius(t.RadiusMD)
			}
		}
		if strings.TrimSpace(it.Text) != "" {
			ui.Column(c).MaxWidth(640).Padding(10, 14).Radius(t.RadiusLG).Background(t.BgRaised).Children(func() {
				if it.Done {
					markdown.View(c, it.Text, &ch.md)
				} else {
					ui.Text(c, it.Text).Selectable().FontSize(t.TextBase)
				}
			})
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, it.Time.Format("15:04")).FontSize(t.TextXS).TextColor(t.FgFaint)
			if strings.TrimSpace(it.Text) != "" {
				ch.copyButton(c, fmt.Sprint("u", it.Key), it.Text, ch.app.T("chat.copyMessage"))
			}
		})
	})
}

// copyButton is a small icon button that copies text and shows a check for
// two seconds.
func (ch *Chat) copyButton(c *ui.Context, id, text, label string) {
	c = c.Key("copy:" + id)
	icon, tip := kit.IconCopy, label
	if time.Since(ch.copied[id]) < 2*time.Second {
		icon, tip = kit.IconCheck, ch.app.T("chat.copied")
	}
	if kit.IconButton(c, icon, tip).Clicked() && copyText(text) {
		ch.copied[id] = time.Now()
		ch.app.Go(func(ctx context.Context) {
			select {
			case <-ctx.Done():
			case <-time.After(2100 * time.Millisecond):
				ch.app.Invalidate()
			}
		})
	}
}

func (ch *Chat) assistantTurn(c *ui.Context, t *kit.Tokens, cv *Conversation, it *Item) {
	blocks := Blocks(it.Parts)
	ui.Column(c).MaxWidth(820).Gap(8).AlignItems(ui.Stretch).Children(func() {
		for i, b := range blocks {
			last := i == len(blocks)-1
			if b.Text != nil {
				ch.textBlock(c.Key(fmt.Sprint("txt", b.Text.Key)), it, b.Text, last)
				continue
			}
			ch.activityGroup(c.Key(fmt.Sprint("grp", b.Events[0].Key)), t, it, b.Events)
		}
		if len(blocks) == 0 && !it.Done {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
				ui.Text(c, ch.app.T("chat.thinking")+"…").FontSize(t.TextXS).TextColor(t.FgMuted)
			})
		}
		if it.Done {
			if txt := it.PlainText(); txt != "" {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ch.copyButton(c, fmt.Sprint("a", it.Key), txt, ch.app.T("chat.copyMessage"))
					ui.Text(c, it.Time.Format("15:04")).FontSize(t.TextXS).TextColor(t.FgFaint)
					if it.Model != "" {
						ui.Text(c, it.Model).FontSize(t.TextXS).TextColor(t.FgFaint)
					}
				})
			}
		}
	})
}

func (ch *Chat) textBlock(c *ui.Context, it *Item, p *Part, last bool) {
	if it.Done || !last {
		markdown.View(c, p.Text, &ch.md)
	} else {
		markdown.Stream(c, p.Text, &ch.md)
	}
}

// activityGroup folds consecutive tool calls and thinking into one row with
// a summary; a lone event shows as its own row.
func (ch *Chat) activityGroup(c *ui.Context, t *kit.Tokens, it *Item, events []*Part) {
	if len(events) == 1 {
		ch.eventRow(c, t, it, events[0])
		return
	}
	first := events[0].Key
	summary, failures, live := GroupSummary(events, ch.app.T)
	open := it.GroupOpen[first]
	ui.Column(c).Gap(4).Children(func() {
		head := ui.Row(c).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(summary)
		head.Children(func() {
			chev := kit.IconChevronR
			if open {
				chev = kit.IconChevronDown
			}
			kit.Icon(c, chev).FontSize(13).TextColor(t.FgFaint)
			ui.Text(c, summary).FontSize(t.TextXS).TextColor(t.FgMuted)
			if failures > 0 {
				ui.Text(c, "· "+ch.app.T("chat.activity.failed", "count", failures)).FontSize(t.TextXS).TextColor(t.Danger)
			}
			if live != nil {
				kit.Spinner(c).FontSize(12).TextColor(t.Accent)
				ui.Text(c, MetaOf(live).ProgressLabel()).FontSize(t.TextXS).TextColor(t.FgFaint).SingleLine().Ellipsis("…").Shrink(1)
			}
		})
		if head.Clicked() {
			if it.GroupOpen == nil {
				it.GroupOpen = map[int]bool{}
			}
			it.GroupOpen[first] = !open
		}
		if open {
			ui.Column(c).Gap(4).Padding(0, 0, 0, 14).Children(func() {
				for _, e := range events {
					ch.eventRow(c.Key(fmt.Sprint("ev", e.Key)), t, it, e)
				}
			})
		}
	})
}

// eventRow is one tool call or thought: a header, and the detail when open.
func (ch *Chat) eventRow(c *ui.Context, t *kit.Tokens, it *Item, p *Part) {
	var icon, label, summary string
	var live, failed bool
	var meta ToolMeta
	open := p.Open
	if p.Kind == PTool {
		meta = MetaOf(p.Tool)
		icon, label, summary = meta.Icon, meta.Label, meta.Summary
		live, failed = p.Tool.Live(), p.Tool.Failed()
		open = p.Tool.Open
	} else {
		icon, label = kit.IconThinking, ch.app.T("chat.thought")
		if !it.Done && p == it.Parts[len(it.Parts)-1] {
			label, live = ch.app.T("chat.thinking"), true
		}
	}
	ui.Column(c).Radius(t.RadiusSM).Border(1, t.Border).Background(t.BgCard).Children(func() {
		head := ui.Row(c).Gap(8).Padding(5, 10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
			Label(label + " " + summary)
		head.Children(func() {
			kit.Icon(c, icon).FontSize(13).TextColor(t.FgFaint)
			ui.Text(c, label).FontSize(t.TextXS).FontWeight(600).Font(t.MonoFont).SingleLine()
			ui.Text(c, summary).FontSize(t.TextXS).TextColor(t.FgMuted).Font(t.MonoFont).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
			switch {
			case live:
				kit.Spinner(c).FontSize(13).TextColor(t.Accent)
			case failed:
				kit.Icon(c, kit.IconError).FontSize(13).TextColor(t.Danger)
			case p.Kind == PTool && p.Tool.Status == "completed":
				kit.Icon(c, kit.IconCheck).FontSize(13).TextColor(t.Success)
			}
			chev := kit.IconChevronR
			if open {
				chev = kit.IconChevronDown
			}
			kit.Icon(c, chev).FontSize(13).TextColor(t.FgFaint)
		})
		if head.Clicked() {
			if p.Kind == PTool {
				p.Tool.Open = !p.Tool.Open
			} else {
				p.Open = !p.Open
			}
		}
		if !open {
			return
		}
		ui.Column(c).Padding(6, 10, 10, 10).Gap(6).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			if p.Kind == PThinking {
				ui.Text(c, p.Text).FontSize(t.TextXS).TextColor(t.FgMuted).Selectable()
				return
			}
			ch.toolDetail(c, t, p.Tool, meta)
		})
	})
}

func label(c *ui.Context, t *kit.Tokens, text string, col ui.Color) {
	ui.Text(c, strings.ToUpper(text)).FontSize(10).FontWeight(600).LetterSpacing(0.5).TextColor(col)
}

func (ch *Chat) block(c *ui.Context, t *kit.Tokens, title, body string, tone ui.Color, maxH float32) {
	if body == "" {
		return
	}
	ui.Column(c).Gap(2).Children(func() {
		if title != "" {
			label(c, t, title, tone)
		}
		sc := ui.Scroll(c).MaxHeight(maxH)
		sc.Children(func() {
			ui.Text(c, clip(body, 8000)).FontSize(t.TextXS).Font(t.MonoFont).TextColor(tone).Selectable()
		})
	})
}

func (ch *Chat) toolDetail(c *ui.Context, t *kit.Tokens, tl *Tool, meta ToolMeta) {
	in := tl.InputMap()
	if len(tl.Locations) > 0 {
		ui.Row(c).Gap(6).Children(func() {
			for i, l := range tl.Locations {
				if i >= 4 {
					break
				}
				kit.Badge(c.Key(fmt.Sprint("loc", i)), shortPath(l.Path), kit.ToneNeutral)
			}
		})
	}
	if len(tl.Images) > 0 {
		for i, src := range tl.Images {
			if bmp := ch.bitmap(src); bmp != nil {
				ui.Image(c.Key(fmt.Sprint("timg", i)), bmp).MaxWidth(480).MaxHeight(320).Fit(ui.Contain).Radius(t.RadiusSM)
			}
		}
		ch.block(c, t, "", tl.Output, t.FgMuted, 120)
		return
	}
	outTone, outTitle := t.Fg, ch.app.T("chat.tool.output")
	if tl.IsError {
		outTone, outTitle = t.Danger, ch.app.T("chat.tool.error")
	}
	switch meta.Class {
	case ClassExec:
		if cmd := str(in, "command", "cmd"); cmd != "" {
			ch.block(c, t, ch.app.T("chat.tool.command"), "$ "+cmd, t.Fg, 160)
		}
		ch.block(c, t, outTitle, tl.Output, outTone, 300)
		ch.exitCode(c, t, tl)
	case ClassEdit:
		oldS, newS := str(in, "old_string", "old_str"), str(in, "new_string", "new_str")
		p := str(in, "file_path", "path")
		if tl.Diff != nil {
			oldS, newS, p = tl.Diff.OldString, tl.Diff.NewString, firstNonEmpty(tl.Diff.FilePath, p)
		}
		ch.diffBlock(c, t, tl.ID, p, oldS, newS)
		ch.resultLine(c, t, tl)
	case ClassWrite:
		content := str(in, "content", "file_text", "new_str")
		p := str(in, "file_path", "path")
		if tl.Diff != nil {
			content, p = firstNonEmpty(tl.Diff.NewString, content), firstNonEmpty(tl.Diff.FilePath, p)
		}
		ch.diffBlock(c, t, tl.ID, p, "", content)
		ch.resultLine(c, t, tl)
	case ClassRead:
		if p := str(in, "file_path", "path"); p != "" {
			ui.Text(c, p).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted).Selectable()
		}
		ch.block(c, t, "", tl.Output, t.FgMuted, 300)
	case ClassSearch, ClassFetch:
		for _, k := range []string{"pattern", "query", "path", "glob", "url"} {
			if v := str(in, k); v != "" {
				ui.Text(c, k+": "+v).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted).Selectable()
			}
		}
		ch.block(c, t, outTitle, tl.Output, outTone, 300)
	default:
		if len(in) > 0 {
			b, _ := json.MarshalIndent(in, "", "  ")
			ch.block(c, t, ch.app.T("chat.tool.input"), string(b), t.FgMuted, 200)
		} else if tl.Input != "" {
			ch.block(c, t, ch.app.T("chat.tool.input"), tl.Input, t.FgMuted, 200)
		}
		ch.block(c, t, outTitle, tl.Output, outTone, 300)
		ch.exitCode(c, t, tl)
	}
}

func (ch *Chat) exitCode(c *ui.Context, t *kit.Tokens, tl *Tool) {
	if tl.Terminal != nil {
		ui.Text(c, ch.app.T("chat.tool.exitCode", "code", tl.Terminal.ExitCode)).FontSize(t.TextXS).TextColor(t.FgFaint)
	}
}

func (ch *Chat) resultLine(c *ui.Context, t *kit.Tokens, tl *Tool) {
	if tl.Output == "" {
		return
	}
	col := t.FgFaint
	if tl.IsError {
		col = t.Danger
	}
	ui.Text(c, clip(tl.Output, 600)).FontSize(t.TextXS).TextColor(col).Selectable()
}

// diffBlock shows an old -> new replacement with the editor's diff viewer
// (unified, collapsed context), kept per tool call.
func (ch *Chat) diffBlock(c *ui.Context, t *kit.Tokens, id, path, oldS, newS string) {
	if path != "" {
		add, rem := countLines(newS), countLines(oldS)
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, path).FontSize(t.TextXS).Font(t.MonoFont).TextColor(t.FgMuted).Selectable()
			ui.Text(c, fmt.Sprintf("-%d", rem)).FontSize(t.TextXS).TextColor(t.Danger)
			ui.Text(c, fmt.Sprintf("+%d", add)).FontSize(t.TextXS).TextColor(t.Success)
		})
	}
	if oldS == "" && newS == "" {
		return
	}
	d := ch.inline[id]
	if d == nil || d.old != oldS || d.new != newS {
		d = &inlineDiff{old: oldS, new: newS, state: newDiffState(path, oldS, newS, editor.DiffUnified)}
		d.state.FitContent = true
		ch.inline[id] = d
	}
	ui.Column(c).Radius(t.RadiusSM).Border(1, t.Border).Children(func() {
		editor.DiffView(c, d.state).FillWidth()
	})
}

func newDiffState(path, oldS, newS string, layout editor.DiffLayout) *editor.DiffState {
	lang := editor.LanguageFor(path)
	before, after := editor.NewBuffer(oldS), editor.NewBuffer(newS)
	before.SetLanguage(lang)
	after.SetLanguage(lang)
	return &editor.DiffState{Old: before, New: after, Layout: layout}
}

// bitmap decodes a data URL once.
func (ch *Chat) bitmap(src string) *ui.Bitmap {
	if b, ok := ch.bitmaps[src]; ok {
		return b
	}
	var bmp *ui.Bitmap
	if _, data, ok := strings.Cut(src, ";base64,"); ok && strings.HasPrefix(src, "data:image/") {
		if raw, err := base64.StdEncoding.DecodeString(data); err == nil {
			bmp, _ = ui.DecodeBitmap(raw)
		}
	}
	ch.bitmaps[src] = bmp
	return bmp
}

func (ch *Chat) routingRow(c *ui.Context, t *kit.Tokens, it *Item) {
	r := it.Routing
	warn := r.Kind == "failover" || r.Kind == "router_unavailable" || r.Kind == "route_unusable"
	icon := icoSparkles
	if warn {
		icon = kit.IconWarning
	}
	muted(c, t, icon, routingLabel(r, strings.TrimSpace(it.Text)), warn)
}

// routingLabel is the compact routing text: "Auto → model · route · p=0.93".
func routingLabel(r *client.RoutingInfo, fallback string) string {
	switch r.Kind {
	case "routed":
		parts := []string{"Auto → " + firstNonEmpty(r.Model, "?")}
		if r.RouteID != "" {
			parts = append(parts, r.RouteID)
		}
		if r.Probability != nil {
			parts = append(parts, fmt.Sprintf("p=%.2f", *r.Probability))
		}
		return strings.Join(parts, " · ")
	case "failover":
		return firstNonEmpty(fallback, "Auto → "+firstNonEmpty(r.Model, "?")+" (failover)")
	}
	return firstNonEmpty(fallback, "Auto → "+firstNonEmpty(r.Model, "?"))
}

var filterSourceOrder = []string{"code", "kb", "events", "memory"}

// contextFilterRow is the "Context filter: kept 4/9 · 38 ms" row, expandable
// to the per-source counts.
func (ch *Chat) contextFilterRow(c *ui.Context, t *kit.Tokens, it *Item) {
	f := it.ContextFilter
	partial := f.Reason != ""
	var sources []string
	for _, s := range filterSourceOrder {
		if _, ok := f.BySource[s]; ok {
			sources = append(sources, s)
		}
	}
	var extra []string
	for s := range f.BySource {
		if !contains(filterSourceOrder, s) {
			extra = append(extra, s)
		}
	}
	sort.Strings(extra)
	sources = append(sources, extra...)
	detailed := len(sources) > 0 || f.RouterModel != "" || partial
	summary := ch.app.T("chat.contextFilter.summary", "kept", f.Kept, "total", f.Kept+f.Dropped)
	if f.LatencyMs != nil {
		summary += fmt.Sprintf(" · %.0f ms", *f.LatencyMs)
	}
	col, icon := t.FgMuted, icoFilter
	if partial {
		col, icon = t.Warning, kit.IconWarning
	}
	ui.Column(c).Gap(2).Children(func() {
		row := ui.Row(c).Gap(6).AlignItems(ui.Center).Label(summary)
		if detailed {
			row.Cursor(ui.CursorPointer).Focusable()
		}
		row.Children(func() {
			kit.Icon(c, icon).FontSize(12).TextColor(col)
			ui.Text(c, summary).FontSize(t.TextXS).TextColor(col).Selectable()
			if detailed {
				chev := kit.IconChevronR
				if it.Open {
					chev = kit.IconChevronDown
				}
				kit.Icon(c, chev).FontSize(12).TextColor(t.FgFaint)
			}
		})
		if detailed && row.Clicked() {
			it.Open = !it.Open
		}
		if it.Open && detailed {
			ui.Column(c).Padding(0, 0, 0, 18).Gap(1).Children(func() {
				line := func(s string) { ui.Text(c, s).FontSize(t.TextXS).TextColor(t.FgMuted).Selectable() }
				for _, s := range sources {
					cn := f.BySource[s]
					line(ch.app.T("chat.contextFilter.sources."+s) + ": " + ch.app.T("chat.contextFilter.keptOf", "kept", cn.Kept, "total", cn.Kept+cn.Dropped))
				}
				if f.Threshold != nil {
					line(ch.app.T("chat.contextFilter.threshold", "value", *f.Threshold))
				}
				if f.RouterModel != "" {
					rm := f.RouterModel
					if f.RouterProvider != "" {
						rm = f.RouterProvider + "/" + rm
					}
					line(ch.app.T("chat.contextFilter.router") + ": " + rm)
				}
				if partial {
					line(ch.app.T("chat.contextFilter.partial", "reason", strings.TrimPrefix(f.Reason, "partial:")))
				}
			})
		}
	})
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func (ch *Chat) personaRow(c *ui.Context, t *kit.Tokens, it *Item) {
	title := ch.app.T("chat.persona")
	ui.Column(c).Radius(t.RadiusSM).Border(1, t.Border).Background(t.BgCard).Children(func() {
		head := ui.Row(c).Gap(8).Padding(5, 10).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(title)
		head.Children(func() {
			kit.Icon(c, icoPersona).FontSize(13).TextColor(t.FgFaint)
			ui.Text(c, title).FontSize(t.TextXS).FontWeight(600).Font(t.MonoFont)
			ui.Text(c, it.Time.Format("15:04")).FontSize(t.TextXS).TextColor(t.FgMuted).Grow(1)
			chev := kit.IconChevronR
			if it.Open {
				chev = kit.IconChevronDown
			}
			kit.Icon(c, chev).FontSize(13).TextColor(t.FgFaint)
		})
		if head.Clicked() {
			it.Open = !it.Open
		}
		if it.Open {
			ui.Column(c).Padding(6, 10, 10, 10).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
				ch.block(c, t, "", it.Text, t.FgMuted, 300)
			})
		}
	})
}
