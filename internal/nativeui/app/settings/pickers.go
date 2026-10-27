package settings

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pickFolder asks for a directory with the native dialog. It blocks, so it
// runs off the UI thread; the dialog opens at the working directory.
var pickFolder = func(title, start string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, DefaultPath: start, Directory: true, CreateDirectories: true})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// relativeTo returns path relative to base when it can ("./.kb",
// "../shared/kb"), the path itself otherwise.
func relativeTo(base, path string) string {
	if base == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "."
	}
	if !strings.HasPrefix(rel, "..") {
		rel = "." + string(filepath.Separator) + rel
	}
	return filepath.ToSlash(rel)
}

// folderControl is a text input for a directory plus a Browse button that
// opens the native folder dialog at the working directory; the choice is
// stored relative to it.
func (s *Settings) folderControl(c *ui.Context, value *string, w float32, placeholder string) ui.Element {
	row := ui.Row(c).Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		kit.Input(c.Key("in"), value, placeholder).Width(w - 96)
		if kit.SecondaryButton(c.Key("browse"), "Browse…").Clicked() {
			a := s.app
			cur := strings.TrimSpace(*value)
			a.Go(func(ctx context.Context) {
				wd := ""
				if ds, err := a.Client.GetDesktopSettings(ctx); err == nil {
					wd = ds.WorkingDirectory
				}
				start := wd
				if cur != "" {
					if filepath.IsAbs(cur) {
						start = cur
					} else if wd != "" {
						start = filepath.Join(wd, cur)
					}
				}
				dir, err := pickFolder("Select a folder", start)
				a.Update(func() {
					switch {
					case err != nil:
						a.Toasts.Error("Could not open the folder dialog", err.Error())
					case dir != "":
						*value = relativeTo(wd, dir)
					}
				})
			})
		}
	})
	return row
}

// judgeTemplateField is the judge prompt template path with the built-in
// default shown on demand (the Web UI's JudgePromptTemplateField).
func judgeTemplateField(s *Settings) Field {
	var show bool
	var def, defErr string
	return Field{
		Path: "judgePromptTemplate", Label: "Judge prompt template file", Kind: KText, Width: 360,
		Desc:        "Path to a custom Go template file (.md or .txt). Leave empty to use the built-in default template.",
		Placeholder: "Leave empty to use built-in default template",
		Extra: func(c *ui.Context, d *Doc) {
			t := kit.Current()
			ui.Column(c).Padding(0, 16, 12, 16).Gap(8).Children(func() {
				label := "Show built-in default template"
				if show {
					label = "Hide built-in default template"
				}
				if kit.Button(c.Key("toggle"), kit.ButtonOpts{Label: label, Variant: kit.Ghost, Size: kit.SizeSM}).Clicked() {
					show = !show
					if show && def == "" {
						a := s.app
						a.Go(func(ctx context.Context) {
							tpl, err := a.Client.DefaultJudgePrompt(ctx)
							a.Update(func() {
								def, defErr = tpl, ""
								if err != nil {
									defErr = err.Error()
								}
							})
						})
					}
				}
				if !show {
					return
				}
				switch {
				case defErr != "":
					ui.Text(c, defErr).FontSize(t.TextSM).TextColor(t.Danger)
				case def == "":
					ui.Text(c, s.app.T("common.loading")).FontSize(t.TextSM).TextColor(t.FgMuted)
				default:
					ui.TextArea(c.Key("tpl"), &def).ReadOnly(true).Lines(12, 24).Font(t.MonoFont).FontSize(t.TextXS)
				}
			})
		},
	}
}
