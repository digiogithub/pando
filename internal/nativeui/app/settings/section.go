package settings

import (
	"github.com/digiogithub/pando/internal/nativeui/kit"
	"github.com/egoist/mygo/ui"
)

// status is the load/save state every section carries.
type status struct {
	loading, saving, loaded bool
	err                     string
}

// St returns the state (part of the section interface).
func (s *status) St() *status { return s }

// section is one settings category. All methods run on the UI thread; the
// network work goes through Settings.app.Go + Update.
type section interface {
	// Load (re)fetches the section's data. Called when the category opens.
	Load()
	Build(c *ui.Context)
	// Dirty reports unsaved edits (the unsaved-changes guard).
	Dirty() bool
	// Save persists the edits through the same PUT endpoint as the Web UI;
	// done reports success (after the toast / error state is set).
	Save(done func(ok bool))
	Discard()
	St() *status
}

// selfManaged sections draw their own save buttons (providers, MCP servers,
// skills...) and so the host omits the generic Save / Reset bar.
type selfManaged interface{ SelfManaged() }

// category is an entry of the settings navigation.
type category struct {
	id       string
	labelKey string
	group    string // "" or "services"
	// path is the configuration path an extension UI policy can hide.
	path string
	make func(s *Settings) section
}

// actionBar draws the Save / Reset buttons of a dirty-tracked section.
func (s *Settings) actionBar(c *ui.Context, sec section) {
	st := sec.St()
	ui.Column(c).Gap(8).Margin(16, 0, 0, 0).Children(func() {
		if st.err != "" {
			s.errorBanner(c, st.err)
		}
		ui.Row(c).Gap(8).Children(func() {
			label := s.app.T("common.save")
			if st.saving {
				label = s.app.T("common.saving")
			}
			if kit.Button(c.Key("save"), kit.ButtonOpts{Label: label, Variant: kit.Primary, Disabled: !sec.Dirty() || st.saving}).Clicked() {
				sec.Save(func(bool) {})
			}
			if kit.Button(c.Key("reset"), kit.ButtonOpts{Label: s.app.T("common.reset"), Variant: kit.Secondary, Disabled: !sec.Dirty()}).Clicked() {
				sec.Discard()
			}
		})
	})
}

// errorBanner shows an error in the danger colour.
func (s *Settings) errorBanner(c *ui.Context, msg string) {
	t := kit.Current()
	ui.Row(c).Padding(10, 12).Radius(t.RadiusMD).Background(t.Danger.Alpha(0.12)).Border(1, t.Danger).Children(func() {
		ui.Text(c, msg).FontSize(t.TextSM).TextColor(t.Danger)
	})
}
