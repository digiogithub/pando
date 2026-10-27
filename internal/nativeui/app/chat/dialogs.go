package chat

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// executeUnsandboxed is the action of a request to run a bash command once
// outside the host sandbox.
const executeUnsandboxed = "execute_unsandboxed"

// blockingDialog shows a modal panel the agent is blocked on: a click
// outside does nothing and Escape is reported (escaped) so the caller can
// fail closed. The panel is built by body and footer.
func blockingDialog(c *ui.Context, t *kit.Tokens, width float32, title func(), body, footer func()) (escaped bool) {
	ui.Overlay(c, func() {
		back := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Background(t.Scrim).Modal()
		escaped = back.OverlayShortcut(0, ui.KeyEscape)
		back.Children(func() {
			ui.Column(c).Width(width).MaxHeightPercent(90).Padding(20).Gap(12).Radius(t.RadiusLG).
				Background(t.BgOverlay).Border(1, t.Border).Role(ui.RoleDialog).
				Shadow(0, 24, 56, 0, ui.RGBA(0, 0, 0, 0.18)).Children(func() {
				title()
				body()
				ui.Row(c).Gap(8).Justify(ui.End).Margin(8, 0, 0, 0).Children(footer)
			})
		})
	})
	return escaped
}

func commandOf(r client.PermissionRequest) string {
	var p struct {
		Command string `json:"command"`
	}
	if len(r.Params) > 0 && json.Unmarshal(r.Params, &p) == nil {
		return p.Command
	}
	return ""
}

func justificationOf(r client.PermissionRequest) string {
	if r.Justification != "" {
		return r.Justification
	}
	var p struct {
		Justification string `json:"justification"`
	}
	if len(r.Params) > 0 && json.Unmarshal(r.Params, &p) == nil {
		return p.Justification
	}
	return ""
}

// dialogs shows the first pending permission request, else the first
// pending question (one at a time, as the Web UI).
func (ch *Chat) dialogs(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	switch {
	case len(cv.Permissions) > 0:
		ch.permissionDialog(c, t, cv, cv.Permissions[0])
	case len(cv.Questions) > 0:
		ch.questionDialog(c, t, cv, cv.Questions[0])
	}
}

// permissionDialog is the fail-closed permission prompt: no button is the
// default (initial focus sits on Deny, nothing is styled primary), Escape
// denies and a click outside does nothing.
func (ch *Chat) permissionDialog(c *ui.Context, t *kit.Tokens, cv *Conversation, req client.PermissionRequest) {
	c = c.Key("perm:" + req.ID)
	escalation := req.Action == executeUnsandboxed
	var esc bool
	esc = blockingDialog(c, t, 520, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if escalation {
				kit.Icon(c, kit.IconWarning).FontSize(18).TextColor(t.Warning)
				ui.Text(c, ch.app.T("chat.permission.escalationTitle")).FontSize(t.TextLG).FontWeight(600).TextColor(t.Warning)
			} else {
				kit.Icon(c, icoShield).FontSize(18).TextColor(t.Accent)
				ui.Text(c, ch.app.T("chat.permission.title")).FontSize(t.TextLG).FontWeight(600)
			}
		})
	}, func() {
		if escalation {
			ui.Text(c, ch.app.T("chat.permission.escalationWarning")).FontSize(t.TextSM).
				Padding(8, 10).Radius(t.RadiusSM).Background(t.WarningSoft).TextColor(t.Warning)
		}
		kv := func(k, v string, mono bool) {
			ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Text(c, k).FontSize(t.TextSM).TextColor(t.FgMuted).Width(90).Shrink(0)
				x := ui.Text(c, v).FontSize(t.TextSM).Selectable().Grow(1).Shrink(1)
				if mono {
					x.Font(t.MonoFont)
				}
			})
		}
		kv(ch.app.T("chat.permission.tool"), req.ToolName, true)
		if req.Action != "" && !escalation {
			kv(ch.app.T("chat.permission.action"), req.Action, false)
		}
		if req.Path != "" {
			kv(ch.app.T("chat.permission.path"), req.Path, true)
		}
		if j := justificationOf(req); escalation && j != "" {
			kv(ch.app.T("chat.permission.justification"), j, false)
		}
		if escalation {
			if cmd := commandOf(req); cmd != "" {
				ui.Column(c).Gap(2).Children(func() {
					label(c, t, ch.app.T("chat.permission.command"), t.FgFaint)
					ui.Scroll(c).MaxHeight(160).Children(func() {
						ui.Text(c, "$ "+cmd).FontSize(t.TextXS).Font(t.MonoFont).Selectable().
							Padding(8, 10).Radius(t.RadiusSM).Background(t.BgCard)
					})
				})
			}
			note := ch.app.T("chat.permission.sessionScopeExact")
			if prefix, ok := strings.CutPrefix(req.GrantKey, "prefix:"); ok {
				note = ch.app.T("chat.permission.sessionScopePrefix", "prefix", prefix)
			}
			ui.Text(c, note).FontSize(t.TextXS).TextColor(t.FgMuted)
		} else if req.Description != "" {
			ui.Text(c, req.Description).FontSize(t.TextSM).Selectable()
		}
		if n := len(cv.Permissions) - 1; n > 0 {
			ui.Text(c, ch.app.T("chat.permission.queued", "count", n)).FontSize(t.TextXS).TextColor(t.FgMuted)
		}
	}, func() {
		deny := kit.Button(c.Key("deny"), kit.ButtonOpts{Variant: kit.Outline, Label: ch.app.T("chat.permission.deny")}).AutoFocus()
		if deny.Clicked() {
			ch.answerPermission(cv, req, "deny")
		}
		if !req.NeverAutoApprove {
			if kit.Button(c.Key("session"), kit.ButtonOpts{Variant: kit.Secondary, Label: ch.app.T("chat.permission.allowSession")}).Clicked() {
				ch.answerPermission(cv, req, "allow_session")
			}
		}
		allow := ch.app.T("chat.permission.allow")
		if escalation {
			allow = ch.app.T("chat.permission.runOnce")
		}
		if kit.Button(c.Key("allow"), kit.ButtonOpts{Variant: kit.Secondary, Label: allow}).Clicked() {
			ch.answerPermission(cv, req, "allow")
		}
	})
	if esc {
		ch.answerPermission(cv, req, "deny")
	}
}

func (ch *Chat) answerPermission(cv *Conversation, req client.PermissionRequest, action string) {
	cv.Resolve(req.ID)
	sid := cmp.Or(req.SessionID, cv.ID)
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.RespondPermission(ctx, sid, req.ID, action); err != nil {
			ch.app.Toasts.Error("Could not answer the permission request", err.Error())
		}
	})
}

// qState is the answers being given to one question request.
type qState struct {
	index    int
	summary  bool
	selected map[int][]string
	other    map[int]string
}

func (s *qState) toggle(i int, label string, multi bool) {
	cur := s.selected[i]
	for j, l := range cur {
		if l == label {
			s.selected[i] = append(cur[:j:j], cur[j+1:]...)
			return
		}
	}
	if multi {
		s.selected[i] = append(cur[:len(cur):len(cur)], label)
	} else {
		s.selected[i] = []string{label}
	}
}

func (s *qState) has(i int, label string) bool {
	for _, l := range s.selected[i] {
		if l == label {
			return true
		}
	}
	return false
}

// answers builds the response, one entry per question.
func (s *qState) answers(req client.QuestionRequest) []client.QuestionAnswer {
	out := make([]client.QuestionAnswer, len(req.Questions))
	for i, q := range req.Questions {
		out[i] = client.QuestionAnswer{QuestionID: q.ID, Selected: append([]string{}, s.selected[i]...), OtherText: strings.TrimSpace(s.other[i])}
	}
	return out
}

// questionDialog is the AskUserQuestion stepper: one question at a time
// (single or multi select, plus a free-text "other"), a review page and
// confirm. No button is the default and Escape cancels the question.
func (ch *Chat) questionDialog(c *ui.Context, t *kit.Tokens, cv *Conversation, req client.QuestionRequest) {
	c = c.Key("ask:" + req.ID)
	st := ch.qstate[req.ID]
	if st == nil {
		st = &qState{selected: map[int][]string{}, other: map[int]string{}}
		ch.qstate[req.ID] = st
	}
	n := len(req.Questions)
	st.index = min(st.index, n-1)
	q := req.Questions[st.index]
	esc := blockingDialog(c, t, 560, func() {
		ui.Column(c).Gap(2).Children(func() {
			if !st.summary {
				eyebrow := ch.app.T("chat.question.single")
				if n > 1 {
					eyebrow = ch.app.T("chat.question.counter", "current", st.index+1, "total", n)
				}
				if q.Header != "" {
					eyebrow += " · " + q.Header
				}
				ui.Text(c, strings.ToUpper(eyebrow)).FontSize(10).FontWeight(600).LetterSpacing(0.5).TextColor(t.FgFaint)
			}
			title := q.Question
			if st.summary {
				title = ch.app.T("chat.question.reviewTitle")
			}
			ui.Text(c, title).FontSize(t.TextLG).FontWeight(600).Selectable()
		})
	}, func() {
		if st.summary {
			ui.Column(c).Gap(8).Children(func() {
				for i, item := range req.Questions {
					labels := append([]string{}, st.selected[i]...)
					if o := strings.TrimSpace(st.other[i]); o != "" {
						labels = append(labels, ch.app.T("chat.question.otherAnswer", "text", o))
					}
					ans := ch.app.T("chat.question.noSelection")
					if len(labels) > 0 {
						ans = strings.Join(labels, ", ")
					}
					ui.Column(c.Key(fmt.Sprint("sum", i))).Gap(1).Children(func() {
						ui.Text(c, item.Question).FontSize(t.TextSM).FontWeight(600)
						ui.Text(c, "→ "+ans).FontSize(t.TextSM).TextColor(t.FgMuted).Selectable()
					})
				}
			})
			return
		}
		ui.Column(c).Gap(6).Children(func() {
			for oi, opt := range q.Options {
				on := st.has(st.index, opt.Label)
				row := ui.Row(c.Key(fmt.Sprint("opt", st.index, oi))).Gap(10).Padding(8, 10).AlignItems(ui.Start).
					Radius(t.RadiusSM).Border(1, t.Border).Cursor(ui.CursorPointer).Focusable().Label(opt.Label)
				if on {
					row.Background(t.AccentSoft).BorderColor(t.Accent)
				} else if row.Hovered() {
					row.Background(t.BgHover)
				}
				if row.Clicked() {
					st.toggle(st.index, opt.Label, q.MultiSelect)
				}
				row.Children(func() {
					mark := ui.Row(c).Width(16).Height(16).Shrink(0).Margin(1, 0, 0, 0).Border(1.5, t.BorderStrong).
						AlignItems(ui.Center).Justify(ui.Center)
					if q.MultiSelect {
						mark.Radius(4)
					} else {
						mark.Radius(8)
					}
					if on {
						mark.Background(t.Accent).BorderColor(t.Accent)
					}
					mark.Children(func() {
						if on {
							kit.Icon(c, kit.IconCheck).FontSize(11).TextColor(t.AccentFg)
						}
					})
					ui.Column(c).Grow(1).Shrink(1).Children(func() {
						ui.Text(c, opt.Label).FontSize(t.TextSM).FontWeight(600)
						if opt.Description != "" {
							ui.Text(c, opt.Description).FontSize(t.TextXS).TextColor(t.FgMuted)
						}
					})
				})
			}
			o := st.other[st.index]
			kit.Input(c.Key(fmt.Sprint("other", st.index)), &o, ch.app.T("chat.question.otherPlaceholder"))
			st.other[st.index] = o
		})
	}, func() {
		if kit.Button(c.Key("qcancel"), kit.ButtonOpts{Variant: kit.Ghost, Label: ch.app.T("chat.question.cancel")}).Clicked() {
			ch.cancelQuestion(cv, req)
		}
		ui.Spacer(c)
		if st.index > 0 || st.summary {
			if kit.Button(c.Key("qback"), kit.ButtonOpts{Variant: kit.Secondary, Label: ch.app.T("chat.question.back")}).Clicked() {
				if st.summary {
					st.summary, st.index = false, n-1
				} else {
					st.index--
				}
			}
		}
		next := ch.app.T("chat.question.review")
		switch {
		case st.summary:
			next = ch.app.T("chat.question.confirm")
		case st.index < n-1:
			next = ch.app.T("chat.question.next")
		}
		if kit.Button(c.Key("qnext"), kit.ButtonOpts{Variant: kit.Secondary, Label: next}).Clicked() {
			switch {
			case st.summary:
				ch.answerQuestion(cv, req, st.answers(req))
			case st.index < n-1:
				st.index++
			default:
				st.summary = true
			}
		}
	})
	if esc {
		ch.cancelQuestion(cv, req)
	}
}

func (ch *Chat) answerQuestion(cv *Conversation, req client.QuestionRequest, answers []client.QuestionAnswer) {
	cv.Resolve(req.ID)
	delete(ch.qstate, req.ID)
	sid := cmp.Or(req.SessionID, cv.ID)
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.RespondQuestion(ctx, sid, req.ID, answers); err != nil {
			ch.app.Toasts.Error("Could not answer the question", err.Error())
		}
	})
}

func (ch *Chat) cancelQuestion(cv *Conversation, req client.QuestionRequest) {
	cv.Resolve(req.ID)
	delete(ch.qstate, req.ID)
	sid := cmp.Or(req.SessionID, cv.ID)
	ch.app.Go(func(ctx context.Context) {
		if err := ch.app.Client.CancelQuestion(ctx, sid, req.ID); err != nil {
			ch.app.Toasts.Error("Could not cancel the question", err.Error())
		}
	})
}
