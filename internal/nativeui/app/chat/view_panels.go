package chat

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/nativeui/app/extensions"
	"github.com/digiogithub/pando/internal/nativeui/client"
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// banners are the plan, goal and modified-files strips above the transcript
// (web ChatView: GoalStatus, PlanView, FileChangesBar).
func (ch *Chat) banners(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	if cv.Goal == nil && len(cv.Plan) == 0 && len(cv.Files) == 0 {
		return
	}
	centered(c, readingWidth, func() {
		ui.Column(c).Padding(8, gutter, 0, gutter).Gap(6).AlignItems(ui.Stretch).Children(func() {
			if cv.Goal != nil {
				ch.goalBanner(c, t, cv)
			}
			if len(cv.Plan) > 0 {
				ch.planBanner(c, t, cv)
			}
		})
	})
}

func banner(c *ui.Context, t *kit.Tokens) ui.Element {
	return ui.Column(c).Radius(t.RadiusMD).Border(1, t.Border).Background(t.BgCard).Padding(8, 12).Gap(6)
}

func (ch *Chat) goalBanner(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	g := cv.Goal
	tone := map[string]kit.Tone{"running": kit.ToneAccent, "completed": kit.ToneSuccess, "blocked": kit.ToneWarning, "cancelled": kit.ToneDanger}[g.Status]
	banner(c.Key("goal"), t).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, icoTarget).FontSize(14).TextColor(t.FgFaint)
			ui.Text(c, ch.app.T("chat.goal.title")).FontSize(t.TextSM).FontWeight(600)
			kit.Badge(c, ch.app.T("chat.goal.status."+g.Status), tone)
			ui.Spacer(c)
			if g.Status == "running" {
				label := ch.app.T("chat.goal.cancel")
				if ch.goalCancelling {
					label = ch.app.T("chat.goal.cancelling")
				}
				if kit.Button(c, kit.ButtonOpts{Variant: kit.Ghost, Size: kit.SizeSM, Label: label, Disabled: ch.goalCancelling}).Clicked() {
					ch.cancelGoal(cv)
				}
			}
		})
		ui.Text(c, g.Objective).FontSize(t.TextSM).Selectable()
		meta := []string{ch.app.T("chat.goal.iteration", "current", g.Iteration, "max", g.MaxIterations)}
		if g.StartedAt > 0 {
			meta = append(meta, ch.app.T("chat.goal.started", "time", time.Unix(g.StartedAt, 0).Format("2006-01-02 15:04")))
		}
		if g.CompletedAt != nil && *g.CompletedAt > 0 {
			meta = append(meta, ch.app.T("chat.goal.finished", "time", time.Unix(*g.CompletedAt, 0).Format("2006-01-02 15:04")))
		}
		ui.Text(c, strings.Join(meta, "  ·  ")).FontSize(t.TextXS).TextColor(t.FgMuted)
		if g.Progress != "" {
			ui.Text(c, ch.app.T("chat.goal.progress")+": "+g.Progress).FontSize(t.TextXS).Selectable()
		}
		if g.NextStep != "" && g.Status == "running" {
			ui.Text(c, ch.app.T("chat.goal.nextStep")+": "+g.NextStep).FontSize(t.TextXS).Selectable()
		}
	})
}

func (ch *Chat) cancelGoal(cv *Conversation) {
	if cv.ID == "" {
		return
	}
	ch.goalCancelling = true
	id := cv.ID
	ch.app.Go(func(ctx context.Context) {
		g, err := ch.app.Client.CancelGoal(ctx, id)
		ch.app.Update(func() {
			ch.goalCancelling = false
			if err != nil {
				ch.app.Toasts.Error("Could not cancel the goal", err.Error())
				return
			}
			cv.Goal = g
		})
	})
}

// planIcon is the status mark of a plan entry.
func planIcon(c *ui.Context, t *kit.Tokens, status string, size float32) {
	switch status {
	case "completed":
		kit.Icon(c, kit.IconSuccess).FontSize(size).TextColor(t.Success)
	case "in_progress":
		kit.Spinner(c).FontSize(size).TextColor(t.Accent)
	default:
		kit.Icon(c, icoCircle).FontSize(size).TextColor(t.FgFaint)
	}
}

func (ch *Chat) planBanner(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	done, active := 0, ""
	for _, e := range cv.Plan {
		if e.Status == "completed" {
			done++
		}
		if e.Status == "in_progress" && active == "" {
			active = e.Title
		}
	}
	banner(c.Key("plan"), t).Children(func() {
		head := ui.Row(c).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(ch.app.T("chat.info.plan"))
		head.Children(func() {
			chev := kit.IconChevronR
			if ch.planOpen {
				chev = kit.IconChevronDown
			}
			kit.Icon(c, chev).FontSize(13).TextColor(t.FgFaint)
			kit.Icon(c, icoPlan).FontSize(14).TextColor(t.FgFaint)
			ui.Text(c, ch.app.T("chat.info.plan")).FontSize(t.TextSM).FontWeight(600)
			ui.Text(c, fmt.Sprintf("%d/%d", done, len(cv.Plan))).FontSize(t.TextXS).TextColor(t.FgMuted)
			if !ch.planOpen && active != "" {
				ui.Text(c, "· "+active).FontSize(t.TextXS).TextColor(t.FgMuted).SingleLine().Ellipsis("…").Shrink(1)
			}
		})
		if head.Clicked() {
			ch.planOpen = !ch.planOpen
		}
		if ch.planOpen {
			ui.Column(c).Gap(3).Children(func() {
				for i, e := range cv.Plan {
					ui.Row(c.Key(fmt.Sprint("pl", i))).Gap(8).AlignItems(ui.Start).Children(func() {
						planIcon(c, t, e.Status, 14)
						x := ui.Text(c, e.Title).FontSize(t.TextSM).Grow(1).Shrink(1)
						if e.Status == "completed" {
							x.TextColor(t.FgMuted)
						}
					})
				}
			})
		}
	})
}

// filesBar is the "N files changed +a -r" strip above the composer with the
// file chips that open the diff viewer.
func (ch *Chat) filesBar(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	files := cv.FileList()
	if len(files) == 0 {
		return
	}
	add, rem := 0, 0
	for _, f := range files {
		add += f.Additions
		rem += f.Removals
	}
	centered(c, readingWidth, func() { ch.filesBarBody(c, t, cv, files, add, rem) })
}

func (ch *Chat) filesBarBody(c *ui.Context, t *kit.Tokens, cv *Conversation, files []*FileChange, add, rem int) {
	ui.Column(c).Padding(0, gutter, 6, gutter).AlignItems(ui.Stretch).Children(func() {
		banner(c.Key("files"), t).Padding(6, 12).Children(func() {
			head := ui.Row(c).Gap(8).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
				Label(ch.app.T("chat.files.changed", "count", len(files)))
			head.Children(func() {
				chev := kit.IconChevronDown
				if ch.filesCollapsed {
					chev = kit.IconChevronR
				}
				kit.Icon(c, chev).FontSize(13).TextColor(t.FgFaint)
				kit.Icon(c, icoFileCode).FontSize(14).TextColor(t.FgFaint)
				ui.Text(c, ch.app.T("chat.files.changed", "count", len(files))).FontSize(t.TextXS).FontWeight(600)
				if add > 0 {
					ui.Text(c, fmt.Sprintf("+%d", add)).FontSize(t.TextXS).TextColor(t.Success)
				}
				if rem > 0 {
					ui.Text(c, fmt.Sprintf("-%d", rem)).FontSize(t.TextXS).TextColor(t.Danger)
				}
			})
			if head.Clicked() {
				ch.filesCollapsed = !ch.filesCollapsed
			}
			if ch.filesCollapsed {
				return
			}
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, f := range files {
					chip := ui.Row(c.Key("fc:"+f.Path)).Gap(6).Padding(3, 8).Radius(t.RadiusSM).Border(1, t.Border).
						AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().Label(f.Name).
						Tooltip(ch.app.T("chat.files.viewDiff", "path", f.Path))
					if chip.Hovered() {
						chip.Background(t.BgHover)
					}
					if chip.Clicked() {
						ch.openDiff(cv, f)
					}
					chip.Children(func() {
						ui.Text(c, f.Name).FontSize(t.TextXS).Font(t.MonoFont)
						if f.Additions > 0 {
							ui.Text(c, fmt.Sprintf("+%d", f.Additions)).FontSize(t.TextXS).TextColor(t.Success)
						}
						if f.Removals > 0 {
							ui.Text(c, fmt.Sprintf("-%d", f.Removals)).FontSize(t.TextXS).TextColor(t.Danger)
						}
					})
				}
			})
		})
	})
}

// diffViewer is the state of the full-window diff viewer.
type diffViewer struct {
	file    *FileChange
	state   *editor.DiffState
	loading bool
	layout  int // 0 split, 1 unified
}

// openDiff loads the full before/after content of a changed file: agent-vcs
// blobs first, then the file on disk with the session's recorded original
// (or the edits reverted), then the raw snippets (web DiffViewer).
func (ch *Chat) openDiff(cv *Conversation, f *FileChange) {
	dv := &diffViewer{file: f, loading: true}
	ch.diff = dv
	sid, cwd, file := cv.ID, ch.cwd, *f
	ch.app.Go(func(ctx context.Context) {
		oldS, newS := loadDiffContent(ctx, ch.app.Client, sid, cwd, file)
		ch.app.Update(func() {
			if ch.diff != dv {
				return
			}
			dv.loading = false
			dv.state = newDiffState(file.Path, oldS, newS, editor.DiffSplit)
		})
	})
}

// relPath is the path relative to the workspace root, "" when outside it.
func relPath(p, cwd string) string {
	if !strings.HasPrefix(p, "/") {
		return strings.TrimPrefix(p, "./")
	}
	root := strings.TrimRight(cwd, "/")
	if root != "" && strings.HasPrefix(p, root+"/") {
		return p[len(root)+1:]
	}
	return ""
}

func loadDiffContent(ctx context.Context, cl *client.Client, sid, cwd string, f FileChange) (oldS, newS string) {
	rel := relPath(f.Path, cwd)
	if sid != "" {
		if entries, err := cl.AgentVCSDiff(ctx, sid); err == nil {
			for _, e := range entries {
				if e.Path == rel || e.Path == f.Path || strings.HasSuffix(f.Path, "/"+e.Path) {
					blob := func(h string) string {
						if h == "" {
							return ""
						}
						s, _ := cl.VCSBlob(ctx, h)
						return s
					}
					return blob(e.OldHash), blob(e.NewHash)
				}
			}
		}
	}
	if rel != "" {
		if cur, err := cl.ReadFile(ctx, rel); err == nil {
			if sid != "" {
				if orig, found, err := cl.FileOriginal(ctx, sid, f.Path); err == nil && found {
					return orig, cur
				}
			}
			return revertEdits(cur, f.Edits), cur
		}
	}
	return snippetsDiff(f)
}

// revertEdits undoes the edits (newest first) on the current content to
// recover the original.
func revertEdits(cur string, edits []Edit) string {
	text := cur
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		if e.Old == "" {
			if e.New == text {
				text = ""
			}
			continue
		}
		if idx := strings.LastIndex(text, e.New); e.New != "" && idx >= 0 {
			text = text[:idx] + e.Old + text[idx+len(e.New):]
		}
	}
	return text
}

// snippetsDiff replays the recorded edits when nothing else is available.
func snippetsDiff(f FileChange) (string, string) {
	if len(f.Edits) == 0 {
		return "", ""
	}
	if len(f.Edits) == 1 {
		return f.Edits[0].Old, f.Edits[0].New
	}
	orig := f.Edits[0].Old
	cur := orig
	for _, e := range f.Edits {
		if e.Old == "" {
			cur = e.New
			continue
		}
		if idx := strings.Index(cur, e.Old); idx >= 0 {
			cur = cur[:idx] + e.New + cur[idx+len(e.Old):]
		} else {
			cur += "\n" + e.New
		}
	}
	return orig, cur
}

// diffOverlay is the full-window diff viewer; Escape or the close button
// closes it.
func (ch *Chat) diffOverlay(c *ui.Context, t *kit.Tokens) {
	dv := ch.diff
	if dv == nil {
		return
	}
	closed := false
	ui.Overlay(c, func() {
		back := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Background(t.Scrim).Modal()
		if back.OverlayShortcut(0, ui.KeyEscape) {
			closed = true
		}
		back.Children(func() {
			ui.Column(c.Key("diffpanel")).Absolute().Left(24).Top(24).Right(24).Bottom(24).Radius(t.RadiusLG).
				Background(t.Bg).Border(1, t.Border).Children(func() {
				ui.Row(c).Gap(10).Padding(10, 14).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
					kit.Icon(c, icoFileCode).FontSize(16).TextColor(t.FgMuted)
					ui.Text(c, dv.file.Path).FontSize(t.TextSM).Font(t.MonoFont).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
					if dv.state != nil {
						a, d := dv.state.Stats()
						if a > 0 {
							ui.Text(c, fmt.Sprintf("+%d", a)).FontSize(t.TextXS).TextColor(t.Success)
						}
						if d > 0 {
							ui.Text(c, fmt.Sprintf("-%d", d)).FontSize(t.TextXS).TextColor(t.Danger)
						}
						sel := dv.layout
						if ui.Segmented(c, &sel, "Split", "Unified").Label("Layout").Changed() {
							dv.layout = sel
							dv.state.Layout = editor.DiffSplit
							if sel == 1 {
								dv.state.Layout = editor.DiffUnified
							}
						}
					}
					ui.Text(c, ch.app.T("chat.diff.edits", "count", len(dv.file.Edits))).FontSize(t.TextXS).TextColor(t.FgFaint)
					if kit.IconButton(c, kit.IconClose, ch.app.T("chat.diff.close")).Clicked() {
						closed = true
					}
				})
				switch {
				case dv.loading:
					ui.Row(c).Grow(1).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
						kit.Spinner(c).FontSize(20).TextColor(t.FgFaint)
						ui.Text(c, ch.app.T("common.loading")).TextColor(t.FgMuted)
					})
				case dv.state != nil:
					editor.DiffView(c, dv.state).Grow(1).FillWidth()
				}
				ui.Text(c, "Esc "+ch.app.T("chat.diff.escToClose")).FontSize(t.TextXS).TextColor(t.FgFaint).Padding(6, 14)
			})
		})
	})
	if closed {
		ch.diff = nil
	}
}

// infoSidebar is the session info panel (web ChatInfoSidebar): title, usage,
// subagents, plan, modified files.
func (ch *Chat) infoSidebar(c *ui.Context, t *kit.Tokens, cv *Conversation) {
	if !ch.infoOpen {
		ch.verFetched = false
		return
	}
	ch.fetchVersion()
	sess, hasSess := ch.session(cv.ID)
	ui.Column(c).Width(280).Shrink(0).BorderWidth(0, 0, 0, 1).BorderColor(t.Border).Background(t.BgShell).Children(func() {
		ui.Row(c).Padding(10, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Children(func() {
				ui.Text(c, strings.ToUpper(ch.app.T("chat.info.session"))).FontSize(10).FontWeight(600).TextColor(t.FgFaint)
				title := ch.app.T("chat.info.noSession")
				if hasSess && strings.TrimSpace(sess.Title) != "" {
					title = sess.Title
				}
				ui.Text(c, title).FontSize(t.TextSM).FontWeight(600).SingleLine().Ellipsis("…")
			})
			if kit.IconButton(c, icoPanelRX, ch.app.T("chat.info.hide")).Clicked() {
				ch.infoOpen = false
			}
		})
		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Padding(0, 12, 12, 12).Gap(14).AlignItems(ui.Stretch).Children(func() {
				if ch.cwd != "" {
					section(c, t, kit.IconFolder, ch.app.T("chat.info.workingDir"), "", func() {
						ui.Text(c, ch.cwd).FontSize(t.TextXS).Font(t.MonoFont).Selectable()
					})
				}
				if hasSess {
					ch.usageSection(c, t, cv, sess)
				}
				if unfinished, running := ch.subagentCounts(); unfinished > 0 {
					section(c, t, kit.IconBot, ch.app.T("chat.info.subagents"), "", func() {
						infoRow(c, t, ch.app.T("chat.info.runningPending"), fmt.Sprintf("%d / %d", running, unfinished), true)
						for _, tk := range ch.tasks {
							if tk.Status == "running" || tk.Status == "pending" {
								ui.Row(c.Key("task" + tk.ID)).Gap(6).AlignItems(ui.Center).Children(func() {
									if tk.Status == "running" {
										kit.Spinner(c).FontSize(11).TextColor(t.Accent)
									} else {
										kit.Icon(c, icoCircle).FontSize(11).TextColor(t.FgFaint)
									}
									ui.Text(c, cmp.Or(tk.Name, tk.ID)).FontSize(t.TextXS).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
									if tk.CurrentTool != "" {
										ui.Text(c, tk.CurrentTool).FontSize(t.TextXS).TextColor(t.FgFaint).Font(t.MonoFont).SingleLine()
									}
								})
							}
						}
					})
				}
				if len(cv.Plan) > 0 {
					done := 0
					for _, e := range cv.Plan {
						if e.Status == "completed" {
							done++
						}
					}
					section(c, t, icoPlan, ch.app.T("chat.info.plan"), fmt.Sprintf("%d/%d", done, len(cv.Plan)), func() {
						for i, e := range cv.Plan {
							ui.Row(c.Key(fmt.Sprint("ip", i))).Gap(6).AlignItems(ui.Start).Children(func() {
								planIcon(c, t, e.Status, 12)
								ui.Text(c, e.Title).FontSize(t.TextXS).Grow(1).Shrink(1)
							})
						}
					})
				}
				files := cv.FileList()
				badge := ""
				if len(files) > 0 {
					badge = fmt.Sprint(len(files))
				}
				section(c, t, icoFileCode, ch.app.T("chat.info.modifiedFiles"), badge, func() {
					if len(files) == 0 {
						ui.Text(c, ch.app.T("chat.info.noModifiedFiles")).FontSize(t.TextXS).TextColor(t.FgMuted)
					}
					for _, f := range files {
						row := ui.Row(c.Key("if:" + f.Path)).Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Focusable().
							Label(f.Path).Tooltip(f.Path)
						if row.Clicked() {
							ch.openDiff(cv, f)
						}
						row.Children(func() {
							ui.Text(c, f.Path).FontSize(t.TextXS).Font(t.MonoFont).SingleLine().Ellipsis("…").Grow(1).Shrink(1)
							if f.Additions > 0 {
								ui.Text(c, fmt.Sprintf("+%d", f.Additions)).FontSize(t.TextXS).TextColor(t.Success)
							}
							if f.Removals > 0 {
								ui.Text(c, fmt.Sprintf("-%d", f.Removals)).FontSize(t.TextXS).TextColor(t.Danger)
							}
						})
					}
				})
				ch.versionSection(c, t)
				// Panels extensions contribute to the chat side (none on
				// standard builds).
				extensions.Slot(ch.app, c, client.SlotChatSide, "", 220)
			})
		})
	})
}

// fetchVersion loads the version status once per opening of the panel.
func (ch *Chat) fetchVersion() {
	if ch.verFetched || ch.app.Client == nil {
		return
	}
	ch.verFetched = true
	a := ch.app
	a.Go(func(ctx context.Context) {
		v, err := a.Client.Version(ctx)
		if err != nil {
			return
		}
		a.Update(func() { ch.version = &v })
	})
}

// versionSection is the running Pando build and, when a newer release
// exists, the command that installs it.
func (ch *Chat) versionSection(c *ui.Context, t *kit.Tokens) {
	v := ch.version
	if v == nil || v.Version == "" {
		return
	}
	section(c, t, kit.IconInfo, ch.app.T("chat.info.version"), "", func() {
		ui.Text(c, "Pando "+v.Version).FontSize(t.TextXS).Font(t.MonoFont).Selectable()
		if v.UpdateAvailable && v.Latest != "" {
			ui.Column(c.Key("update")).Gap(4).Padding(6, 8).Radius(t.RadiusSM).Background(t.AccentSoft).Children(func() {
				ui.Text(c, ch.app.T("version.updateAvailable", "latest", v.Latest)).FontSize(t.TextXS).TextColor(t.Accent)
				ui.Text(c, cmp.Or(v.UpdateCommand, "pando update")).FontSize(t.TextXS).Font(t.MonoFont).Selectable()
			})
		}
	})
}

func section(c *ui.Context, t *kit.Tokens, icon, title, badge string, body func()) {
	ui.Column(c.Key("sec:" + title)).Gap(5).AlignItems(ui.Stretch).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			kit.Icon(c, icon).FontSize(12).TextColor(t.FgFaint)
			ui.Text(c, strings.ToUpper(title)).FontSize(10).FontWeight(600).LetterSpacing(0.5).TextColor(t.FgFaint).Grow(1)
			if badge != "" {
				ui.Text(c, badge).FontSize(t.TextXS).TextColor(t.FgMuted)
			}
		})
		body()
	})
}

func infoRow(c *ui.Context, t *kit.Tokens, k, v string, accent bool) {
	ui.Row(c.Key("ir:" + k)).Gap(8).Children(func() {
		ui.Text(c, k).FontSize(t.TextXS).TextColor(t.FgMuted).Grow(1)
		x := ui.Text(c, v).FontSize(t.TextXS).Font(t.MonoFont)
		if accent {
			x.TextColor(t.Accent)
		}
	})
}

// formatCount renders a token count with K/M suffixes (TUI sidebar).
func formatCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

// contextPct is the share of the context window used (0..100), or -1 when
// the window is unknown.
func contextPct(total, window int64) float64 {
	if window <= 0 {
		return -1
	}
	return min(100*float64(total)/float64(window), 100)
}

func meterColor(t *kit.Tokens, pct float64) ui.Color {
	switch {
	case pct >= 90:
		return t.Danger
	case pct >= 70:
		return t.Warning
	}
	return t.Accent
}

func (ch *Chat) usageSection(c *ui.Context, t *kit.Tokens, cv *Conversation, s client.SessionSummary) {
	total := s.PromptTokens + s.CompletionTokens
	window := cv.ContextWindow
	section(c, t, icoGauge, ch.app.T("chat.info.usage"), "", func() {
		if pct := contextPct(total, window); pct >= 0 {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, ch.app.T("chat.info.context")).FontSize(t.TextXS).TextColor(t.FgMuted).Grow(1)
				ui.Text(c, fmt.Sprintf("%s / %s (%.0f%%)", formatCount(total), formatCount(window), pct)).FontSize(t.TextXS).Font(t.MonoFont)
			})
			ui.Row(c).Height(4).Radius(2).Background(t.BgRaised).Children(func() {
				ui.Box(c).WidthPercent(float32(pct)).Height(4).Radius(2).Background(meterColor(t, pct))
			})
		} else {
			infoRow(c, t, ch.app.T("chat.info.totalTokens"), formatCount(total), false)
		}
		infoRow(c, t, ch.app.T("chat.info.inputOutput"), formatCount(s.PromptTokens)+" / "+formatCount(s.CompletionTokens), false)
		if s.CacheReadTokens > 0 || s.CacheCreationTokens > 0 {
			infoRow(c, t, ch.app.T("chat.info.cache"), formatCount(s.CacheReadTokens)+" / "+formatCount(s.CacheCreationTokens), false)
		}
		if s.ReasoningTokens > 0 {
			infoRow(c, t, ch.app.T("chat.info.reasoning"), formatCount(s.ReasoningTokens), false)
		}
		if s.Cost > 0 {
			infoRow(c, t, ch.app.T("chat.info.cost"), fmt.Sprintf("$%.4f", s.Cost), true)
		}
	})
}

func (ch *Chat) subagentCounts() (unfinished, running int) {
	for _, tk := range ch.tasks {
		if tk.Status == "running" {
			running++
		}
		if tk.Status == "running" || tk.Status == "pending" {
			unfinished++
		}
	}
	return
}
