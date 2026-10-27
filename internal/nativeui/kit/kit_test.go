package kit

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestResolveTokens(t *testing.T) {
	light := Resolve(FamilyPando, false, "")
	if light.Bg != ui.Hex("#fdfcf8") || light.Accent != ui.Hex("#8f6310") {
		t.Errorf("pando light = %+v", light)
	}
	dark := Resolve(FamilyPando, true, "")
	if dark.Bg != ui.Hex("#0c1f18") || dark.AccentFg != ui.Hex("#0f2a20") || dark.Danger != ui.Hex("#f87171") {
		t.Errorf("pando dark = %+v", dark)
	}
	if v := Resolve(FamilySlate, true, "violet"); v.Accent != ui.Hex("#8b7cf6") || v.Bg != ui.Hex("#0f141b") {
		t.Errorf("slate dark violet = %+v", v)
	}
	if u := Resolve("nope", false, ""); u.Family != FamilyPando {
		t.Errorf("unknown family resolved to %q", u.Family)
	}
	th := dark.UI()
	if !th.Dark || th.Background != dark.Bg || th.Accent != dark.Accent || th.AccentText != dark.AccentFg {
		t.Errorf("ui theme not mapped: %+v", th)
	}
}

func TestIconsEmbedded(t *testing.T) {
	for _, n := range []string{IconChat, IconSimpleChat, IconProjects, IconFolder, IconFile, IconFileText, IconEditor,
		IconTerminal, IconOrchestrate, IconDesign, IconLogs, IconAgentVCS, IconEvaluator, IconInstances, IconSettings,
		IconExtensions, IconSetup, IconPlus, IconSend, IconStop, IconClose, IconCheck, IconCopy, IconTrash, IconEdit,
		IconChevronDown, IconChevronUp, IconChevronR, IconChevronL, IconSearch, IconRefresh, IconSpinner, IconError,
		IconWarning, IconInfo, IconSuccess, IconThinking, IconTool, IconSun, IconMoon, IconMonitor, IconPanelLeft,
		IconPanelRight, IconMore, IconArrowDown, IconArrowUp, IconExternal, IconBot, IconUser, IconClock, IconTodo, IconHome} {
		if !HasIcon(n) || SVG(n) == nil {
			t.Errorf("icon %q missing or invalid", n)
		}
	}
	if SVG("no-such-icon") == nil {
		t.Error("unknown icon should fall back")
	}
}

type demo struct {
	clicks int
	on     bool
	open   bool
	name   string
	choice string
}

func (d *demo) view(c *ui.Context) {
	Apply(c, Settings{Family: FamilyPando, Mode: ModeDark})
	ui.Column(c).Fill().Gap(8).Padding(16).Children(func() {
		if PrimaryButton(c, "Save").Clicked() {
			d.clicks++
		}
		if IconButton(c, IconSettings, "Preferences").Clicked() {
			d.open = true
		}
		Badge(c, "beta", ToneAccent)
		Switch(c, &d.on, "Auto approve")
		Input(c, &d.name, "Name")
		Select(c, &d.choice, []string{"pando", "paper"})
		Card(c, func() { ui.Text(c, "In a card") })
		SettingsSection(c, "General", "Basics", func() {
			SettingsRow(c, "Language", "UI language", func() { ui.Text(c, "English") })
		})
		NavItem(c, IconChat, "Chat", true, false)
		EmptyState(c, IconBot, "Nothing yet", "Start a chat")
		Dialog(c, &d.open, DialogOpts{Title: "Preferences", Body: func() { ui.Text(c, "Dialog body") }})
	})
	ToastViewport(c)
}

func TestComponentsHeadless(t *testing.T) {
	d := &demo{}
	tt := ui.NewTester(d.view, 900, 900)
	for _, want := range []string{"Save", "beta", "Auto approve", "In a card", "General", "Language", "Chat", "Nothing yet"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if d.clicks != 1 {
		t.Errorf("clicks = %d", d.clicks)
	}
	if err := tt.Click("Auto approve"); err != nil {
		t.Fatal(err)
	}
	if !d.on {
		t.Error("switch label did not toggle")
	}
	if err := tt.Click("Preferences"); err != nil {
		t.Fatal(err)
	}
	if !d.open || !tt.HasText("Dialog body") {
		t.Errorf("dialog not open: %q", tt.Texts())
	}
	if !Current().Dark {
		t.Error("ModeDark not applied")
	}
}

func TestToasterFlush(t *testing.T) {
	q := &Toaster{}
	q.Error("Boom", "details")
	view := func(c *ui.Context) {
		Apply(c, DefaultSettings)
		q.Flush(c)
		ToastViewport(c)
	}
	tt := ui.NewTester(view, 600, 400)
	tt.Frame()
	if !tt.HasText("Boom") || !tt.HasText("details") {
		t.Errorf("toast not shown: %q", tt.Texts())
	}
}
