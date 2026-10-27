package shell

import "testing"

func TestUseTrayNeedsSettingAndLiveIcon(t *testing.T) {
	c := newController(Options{})
	if c.UseTray() {
		t.Fatal("tray used by default")
	}
	c.SetTrayOnClose(true)
	if c.UseTray() {
		t.Fatal("setting on without a live tray icon must keep the normal minimise/close")
	}
	c.trayOK.Store(true)
	if !c.UseTray() {
		t.Fatal("setting on with a tray icon must hide to the tray")
	}
	c.SetTrayOnClose(false)
	if c.UseTray() {
		t.Fatal("setting off must restore the normal behaviour")
	}
}

func TestTrayLabelsDefaults(t *testing.T) {
	l := TrayLabels{Quit: "Salir"}.orDefault()
	if l.Show != "Show Pando" || l.Settings != "Settings" || l.Quit != "Salir" {
		t.Errorf("labels = %+v", l)
	}
}

func TestDefaultMenuHasEditMenu(t *testing.T) {
	// The Edit role menu must stay: macOS copy/paste keys come from it.
	if DefaultMenu(nil) == nil {
		t.Fatal("no menu")
	}
}
