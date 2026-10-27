package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// sessRow is a row of the sessions list: a date bucket header, a session or
// the load-more button.
type sessRow struct {
	header string
	idx    int // index into ch.sessions, -1 for headers and the more row
	more   bool
}

// bucketOf names the date bucket of a session's last update.
func bucketOf(now, ts time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !ts.Before(today):
		return "Today"
	case !ts.Before(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case !ts.Before(today.AddDate(0, 0, -7)):
		return "Previous 7 days"
	case !ts.Before(today.AddDate(0, 0, -30)):
		return "Previous 30 days"
	}
	return "Older"
}

// matchesSearch reports whether a session matches the search text (title and
// prompt preview, case-insensitive).
func matchesSearch(s client.SessionSummary, q string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	return strings.Contains(strings.ToLower(s.Title), q) || strings.Contains(strings.ToLower(s.PromptPreview), q)
}

// buildRows lays out the filtered sessions under their date buckets.
func (ch *Chat) buildRows(now time.Time) {
	ch.rows = ch.rows[:0]
	last := ""
	for i, s := range ch.sessions {
		if !matchesSearch(s, ch.sessSearch) {
			continue
		}
		if b := bucketOf(now, s.Updated()); b != last {
			ch.rows = append(ch.rows, sessRow{header: b, idx: -1})
			last = b
		}
		ch.rows = append(ch.rows, sessRow{idx: i})
	}
	if ch.sessMore {
		ch.rows = append(ch.rows, sessRow{idx: -1, more: true})
	}
}

func (ch *Chat) sessRowKey(i int) any {
	if i >= len(ch.rows) {
		return i
	}
	r := ch.rows[i]
	switch {
	case r.more:
		return "more"
	case r.idx < 0:
		return "h:" + r.header
	case r.idx < len(ch.sessions):
		return ch.sessions[r.idx].ID
	}
	return i
}

func (ch *Chat) sessionsPane(c *ui.Context, t *kit.Tokens) {
	ch.buildRows(time.Now())
	ui.Column(c).Width(260).Shrink(0).BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Background(t.BgShell).
		AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Padding(10, 12, 4, 12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, ch.app.T("nav.sections.sessions")).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgFaint).Grow(1)
			if ch.sessLoading {
				kit.Spinner(c).FontSize(14).TextColor(t.FgFaint)
			}
			if kit.IconButton(c.Key("sesshide"), kit.IconPanelLeft, ch.app.T("chat.simple.toggleSessions")).Clicked() {
				ch.sessHidden = true
			}
		})
		ui.Row(c).Padding(0, 10, 6, 10).Children(func() {
			kit.Input(c.Key("sesssearch"), &ch.sessSearch, ch.app.T("shell.searchSessions")).Grow(1)
		})
		if ch.sessErr != "" {
			ui.Text(c, ch.sessErr).FontSize(t.TextXS).TextColor(t.Danger).Padding(4, 12)
		}
		list := ui.List(c, &ch.sessList, len(ch.rows), func(i int) {
			r := ch.rows[i]
			switch {
			case r.more:
				ch.moreRow(c, t)
			case r.idx < 0:
				ui.Text(c, strings.ToUpper(r.header)).FontSize(10).FontWeight(600).LetterSpacing(0.5).
					TextColor(t.FgFaint).Padding(8, 8, 2, 8)
			default:
				ch.sessionRow(c, t, ch.sessions[r.idx], r.idx == ch.selected)
			}
		}).Grow(1).Padding(0, 6, 8, 6).Gap(1)
		list.Children(func() {
			if len(ch.rows) == 0 && !ch.sessLoading {
				ui.Text(c, ch.app.T("nav.noSessionsYet")).FontSize(t.TextSM).TextColor(t.FgMuted).Padding(12)
			}
		})
	})
}

func (ch *Chat) moreRow(c *ui.Context, t *kit.Tokens) {
	label := ch.app.T("chat.simple.loadMore", "loaded", len(ch.sessions), "total", ch.sessTotal)
	if kit.Button(c, kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: label, Disabled: ch.sessLoading}).Clicked() {
		ch.loadSessions(ch.sessOffset)
	}
}

func (ch *Chat) sessionRow(c *ui.Context, t *kit.Tokens, s client.SessionSummary, active bool) {
	title := strings.TrimSpace(s.Title)
	if title == "" {
		title = ch.app.T("nav.untitledSession")
	}
	row := ui.Row(c).Gap(8).Padding(7, 10).Radius(t.RadiusSM).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Focusable().Label(title)
	if row.Clicked() && s.ID != ch.current {
		ch.openFromUI(s.ID)
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
		if m.Item(ch.app.T("shell.renameSession")).Chosen() {
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
		Title: ch.app.T("shell.renameSession"),
		Body: func() {
			if kit.Input(c, &ch.renameText, "Title").AutoFocus().Submitted() {
				ch.commitRename()
			}
		},
		Footer: func() {
			if kit.SecondaryButton(c, ch.app.T("projects.view.common.cancel")).Clicked() {
				ch.renaming = false
			}
			if kit.PrimaryButton(c, ch.app.T("projects.view.actions.rename")).Clicked() {
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
			ch.app.Toasts.Error(ch.app.T("shell.renameSessionFailed"), err.Error())
		}
	})
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
