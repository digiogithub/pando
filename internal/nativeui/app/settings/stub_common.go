package settings

import "github.com/egoist/mygo/ui"

// stubSection is a placeholder for a section not built yet.
type stubSection struct {
	status
	s    *Settings
	name string
}

func (x *stubSection) Load()                { x.loaded = true }
func (x *stubSection) Dirty() bool          { return false }
func (x *stubSection) Save(done func(bool)) { done(true) }
func (x *stubSection) Discard()             {}
func (x *stubSection) SelfManaged()         {}
func (x *stubSection) Build(c *ui.Context)  { ui.Text(c, "TODO "+x.name) }
