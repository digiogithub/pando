package orchestrator

import (
	"context"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

type createForm struct {
	open        bool
	name        string
	description string
	model       string // "" = default
	busy        bool
	err         string
}

const defaultModel = "Default model"

func (v *View) createDialog(c *ui.Context, t *kit.Tokens) {
	f := &v.create
	a := v.app
	wasOpen := f.open
	kit.Dialog(c, &f.open, kit.DialogOpts{
		Title: "Create Orchestrator Task",
		Width: 480,
		Body: func() {
			label := func(s string) {
				ui.Text(c, strings.ToUpper(s)).FontSize(t.TextXS).FontWeight(600).TextColor(t.FgMuted).LetterSpacing(0.4)
			}
			ui.Column(c).Gap(14).Children(func() {
				ui.Column(c).Gap(6).Children(func() {
					label("Task Name *")
					kit.Input(c.Key("ct:name"), &f.name, "e.g. Refactor auth module")
				})
				ui.Column(c).Gap(6).Children(func() {
					label("Description / Prompt")
					ui.TextArea(c.Key("ct:desc"), &f.description).Lines(4, 8).Placeholder("Describe what the agent should do...").
						Background(t.BgInput).Border(1, t.BorderStrong).Radius(t.RadiusSM).FontSize(t.TextSM).Padding(8)
				})
				ui.Column(c).Gap(6).Children(func() {
					label("Model")
					sel := f.model
					if sel == "" {
						sel = defaultModel
					}
					opts := append([]string{defaultModel}, v.models...)
					kit.Select(c.Key("ct:model"), &sel, opts)
					if sel == defaultModel {
						f.model = ""
					} else {
						f.model = sel
					}
				})
				if f.err != "" {
					ui.Text(c, f.err).FontSize(t.TextXS).TextColor(t.Danger).Padding(8, 12).Background(t.DangerSoft).Radius(t.RadiusSM)
				}
			})
		},
		Footer: func() {
			if kit.Button(c, kit.ButtonOpts{Label: "Cancel"}).Clicked() {
				f.open = false
			}
			label := "Create Task"
			if f.busy {
				label = "Creating…"
			}
			if kit.Button(c, kit.ButtonOpts{Variant: kit.Primary, Label: label, Disabled: f.busy || strings.TrimSpace(f.name) == ""}).Clicked() {
				v.submitCreate(a)
			}
		},
	})
	if wasOpen && !f.open {
		f.err = ""
	}
}

func (v *View) submitCreate(a interface {
	Go(func(context.Context))
	Update(func())
}) {
	f := &v.create
	prompt := strings.TrimSpace(f.description)
	name := strings.TrimSpace(f.name)
	if prompt == "" {
		prompt = name
	} else {
		prompt = name + "\n\n" + prompt
	}
	model := f.model
	f.busy, f.err = true, ""
	a.Go(func(ctx context.Context) {
		_, err := v.app.Client.CreateOrchestratorTask(ctx, prompt, model, "")
		a.Update(func() {
			f.busy = false
			if err != nil {
				f.err = err.Error()
				return
			}
			f.open, f.name, f.description = false, "", ""
		})
		if err == nil {
			v.refreshTasks(ctx)
		}
	})
}
