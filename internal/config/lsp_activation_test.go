package config

import (
	"testing"

	"github.com/spf13/viper"
)

func TestLSPActivationModeNormalization(t *testing.T) {
	cases := map[string]string{
		"":            LSPActivateEdits,
		"edits":       LSPActivateEdits,
		"  EDITS  ":   LSPActivateEdits,
		"nonsense":    LSPActivateEdits,
		"reads":       LSPActivateReads,
		"Workspace":   LSPActivateWorkspace,
		"off":         LSPActivateOff,
		"WORKSPACE  ": LSPActivateWorkspace,
	}
	for in, want := range cases {
		c := &Config{LSPActivateOn: in}
		if got := c.LSPActivationMode(); got != want {
			t.Errorf("LSPActivationMode(%q) = %q, want %q", in, got, want)
		}
	}
	var nilCfg *Config
	if got := nilCfg.LSPActivationMode(); got != LSPActivateEdits {
		t.Errorf("nil config mode = %q, want %q", got, LSPActivateEdits)
	}
}

func TestLSPActivationAllows(t *testing.T) {
	triggers := []LSPTrigger{LSPTriggerEdit, LSPTriggerRead, LSPTriggerWorkspace, LSPTriggerExplicit}

	cases := []struct {
		name string
		cfg  *Config
		want map[LSPTrigger]bool
	}{
		{
			name: "default edits mode",
			cfg:  &Config{LSPAutoActivate: true},
			want: map[LSPTrigger]bool{
				LSPTriggerEdit: true, LSPTriggerRead: false,
				LSPTriggerWorkspace: false, LSPTriggerExplicit: true,
			},
		},
		{
			name: "reads mode",
			cfg:  &Config{LSPAutoActivate: true, LSPActivateOn: LSPActivateReads},
			want: map[LSPTrigger]bool{
				LSPTriggerEdit: true, LSPTriggerRead: true,
				LSPTriggerWorkspace: false, LSPTriggerExplicit: true,
			},
		},
		{
			name: "workspace mode",
			cfg:  &Config{LSPAutoActivate: true, LSPActivateOn: LSPActivateWorkspace},
			want: map[LSPTrigger]bool{
				LSPTriggerEdit: true, LSPTriggerRead: true,
				LSPTriggerWorkspace: true, LSPTriggerExplicit: true,
			},
		},
		{
			name: "off mode blocks even an explicit request",
			cfg:  &Config{LSPAutoActivate: true, LSPActivateOn: LSPActivateOff},
			want: map[LSPTrigger]bool{
				LSPTriggerEdit: false, LSPTriggerRead: false,
				LSPTriggerWorkspace: false, LSPTriggerExplicit: false,
			},
		},
		{
			name: "LSPAutoActivate=false overrides the mode",
			cfg:  &Config{LSPAutoActivate: false, LSPActivateOn: LSPActivateWorkspace},
			want: map[LSPTrigger]bool{
				LSPTriggerEdit: false, LSPTriggerRead: false,
				LSPTriggerWorkspace: false, LSPTriggerExplicit: false,
			},
		},
		{
			name: "nil config activates nothing",
			cfg:  nil,
			want: map[LSPTrigger]bool{
				LSPTriggerEdit: false, LSPTriggerRead: false,
				LSPTriggerWorkspace: false, LSPTriggerExplicit: false,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, trigger := range triggers {
				if got := tc.cfg.LSPActivationAllows(trigger); got != tc.want[trigger] {
					t.Errorf("trigger %q = %v, want %v", trigger, got, tc.want[trigger])
				}
			}
			wantWatch := tc.want[LSPTriggerWorkspace]
			if got := tc.cfg.LSPWorkspaceWatchEnabled(); got != wantWatch {
				t.Errorf("LSPWorkspaceWatchEnabled = %v, want %v", got, wantWatch)
			}
		})
	}
}

func TestLSPActivateOnDefaultsToEdits(t *testing.T) {
	// The workspace watcher must stay off unless the user opts in, so a fresh
	// configuration never watches the whole tree.
	c := &Config{LSPAutoActivate: true}
	if c.LSPWorkspaceWatchEnabled() {
		t.Fatal("the workspace watcher must be opt-in")
	}
	if !c.LSPActivationAllows(LSPTriggerEdit) {
		t.Fatal("edits must activate by default")
	}
}

func TestUnknownTriggerIsRejected(t *testing.T) {
	c := &Config{LSPAutoActivate: true, LSPActivateOn: LSPActivateWorkspace}
	if c.LSPActivationAllows(LSPTrigger("bogus")) {
		t.Fatal("an unknown trigger must not activate a server")
	}
}

func TestLSPRegistryPropagatesInstallMetadata(t *testing.T) {
	c := &Config{LSP: map[string]LSPConfig{}}

	byName := map[string]ResolvedLSPServer{}
	for _, s := range c.LSPRegistry() {
		byName[s.Name] = s
	}

	pyright, ok := byName["pyright"]
	if !ok {
		t.Fatal("pyright preset missing from the registry")
	}
	if pyright.Install.Strategy != LSPInstallNpm || pyright.Install.Package != "pyright" {
		t.Fatalf("unexpected install metadata: %+v", pyright.Install)
	}
	if pyright.Install.Bin != "pyright-langserver" {
		t.Fatalf("bin = %q, want pyright-langserver", pyright.Install.Bin)
	}

	gopls := byName["gopls"]
	if gopls.Install.Strategy != LSPInstallManual || gopls.Install.Hint == "" {
		t.Fatalf("gopls must be manual with a hint, got %+v", gopls.Install)
	}
}

func TestLSPRegistryDropsInstallWhenUserOverridesCommand(t *testing.T) {
	c := &Config{LSP: map[string]LSPConfig{
		"pyright": {Command: "/opt/my-pyright"},
	}}

	for _, s := range c.LSPRegistry() {
		if s.Name != "pyright" {
			continue
		}
		if s.Command != "/opt/my-pyright" {
			t.Fatalf("command = %q", s.Command)
		}
		if s.Install.Strategy != LSPInstallNone {
			t.Fatalf("a user-provided command must not carry an install recipe, got %+v", s.Install)
		}
		return
	}
	t.Fatal("pyright missing from the registry")
}

func TestLSPRegistryKeepsInstallWhenUserOnlyTogglesFlags(t *testing.T) {
	// Enabling Autostart must not disable the install recipe.
	c := &Config{LSP: map[string]LSPConfig{"pyright": {Autostart: true}}}

	for _, s := range c.LSPRegistry() {
		if s.Name != "pyright" {
			continue
		}
		if !s.Autostart || s.Install.Strategy != LSPInstallNpm {
			t.Fatalf("unexpected server: %+v", s)
		}
		return
	}
	t.Fatal("pyright missing from the registry")
}

func TestLSPHandlesFileByName(t *testing.T) {
	s := ResolvedLSPServer{
		Languages: []string{".dockerfile"},
		Filenames: []string{"Dockerfile", "Containerfile"},
	}
	for _, path := range []string{"Dockerfile", "app/Dockerfile", "Dockerfile.dev", "containerfile", "web.dockerfile"} {
		if !s.HandlesFile(path) {
			t.Errorf("expected %q to be handled", path)
		}
	}
	for _, path := range []string{"Makefile", "main.go", "docker-compose.yml"} {
		if s.HandlesFile(path) {
			t.Errorf("did not expect %q to be handled", path)
		}
	}

	// Extensions alone never claim an extensionless file.
	ext := ResolvedLSPServer{Languages: []string{".go"}}
	if ext.HandlesFile("Makefile") {
		t.Error("an extension-only server must not claim an extensionless file")
	}
	// A server declaring nothing is a catch-all.
	if !(ResolvedLSPServer{}).HandlesFile("Makefile") {
		t.Error("a server with no Languages and no Filenames must handle everything")
	}
}

func TestLSPServersForFileMatchesByName(t *testing.T) {
	c := &Config{LSP: map[string]LSPConfig{}}

	var found bool
	for _, s := range c.LSPServersForFile("Dockerfile") {
		if s.Name == "dockerfile-language-server" {
			found = true
		}
	}
	if !found {
		t.Fatal("Dockerfile must resolve to the dockerfile language server")
	}
	if got := c.LSPServersForFile(""); got != nil {
		t.Fatalf("an empty path must resolve to no server, got %v", got)
	}
}

func TestOptInPresetsStayDisabledUntilDeclared(t *testing.T) {
	byName := func(c *Config, name string) (ResolvedLSPServer, bool) {
		for _, s := range c.LSPRegistry() {
			if s.Name == name {
				return s, true
			}
		}
		return ResolvedLSPServer{}, false
	}

	c := &Config{LSP: map[string]LSPConfig{}}
	biome, ok := byName(c, "biome")
	if !ok {
		t.Fatal("biome preset missing")
	}
	if !biome.Disabled {
		t.Fatal("an opt-in preset must be disabled until the user declares it")
	}
	for _, s := range c.LSPServersForFile("app.ts") {
		if s.Name == "biome" {
			t.Fatal("an opt-in preset must not be a candidate for activation")
		}
	}

	// Declaring the server, even with an empty table, enables it.
	c = &Config{LSP: map[string]LSPConfig{"biome": {}}}
	biome, _ = byName(c, "biome")
	if biome.Disabled {
		t.Fatal("declaring [LSP.biome] must enable the preset")
	}
	if biome.Install.Strategy != LSPInstallNpm || biome.Command != "biome" {
		t.Fatalf("unexpected server: %+v", biome)
	}
}

func TestPresetCatalogueIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	commands := map[string]string{}
	for _, p := range LSPPresets() {
		if seen[p.Name] {
			t.Errorf("duplicate preset name %q", p.Name)
		}
		seen[p.Name] = true

		if p.Config.Command == "" {
			t.Errorf("%s: empty command", p.Name)
		}
		if len(p.Config.Languages) == 0 && len(p.Config.Filenames) == 0 {
			t.Errorf("%s: a preset must declare the files it handles", p.Name)
		}
		if other, dup := commands[p.Config.Command]; dup {
			t.Errorf("%s: command %q already used by %s", p.Name, p.Config.Command, other)
		}
		commands[p.Config.Command] = p.Name

		switch p.Install.Strategy {
		case LSPInstallNpm:
			if p.Install.Package == "" || p.Install.Bin == "" {
				t.Errorf("%s: an npm preset needs a package and a bin", p.Name)
			}
			if p.Install.Bin != p.Config.Command {
				t.Errorf("%s: install bin %q must be the configured command %q", p.Name, p.Install.Bin, p.Config.Command)
			}
			if p.Install.Hint == "" {
				t.Errorf("%s: missing manual install hint", p.Name)
			}
		case LSPInstallManual:
			if p.Install.Hint == "" || p.Install.URL == "" {
				t.Errorf("%s: a manual preset needs a hint and a URL", p.Name)
			}
		default:
			t.Errorf("%s: every preset needs an install strategy", p.Name)
		}
	}
}

func TestLSPActivationSettingsRoundTrip(t *testing.T) {
	c := &Config{LSPAutoActivate: true, LSPActivateOn: "  WORKSPACE ", LSPAutoInstall: true}
	got := c.LSPActivationSettings()
	if got.ActivateOn != LSPActivateWorkspace {
		t.Fatalf("activateOn = %q", got.ActivateOn)
	}
	if got.StartupTimeout != "20s" || got.InstallTimeout != "2m0s" {
		t.Fatalf("timeouts = %q / %q, want the effective defaults", got.StartupTimeout, got.InstallTimeout)
	}

	var nilCfg *Config
	if def := nilCfg.LSPActivationSettings(); def.ActivateOn != LSPActivateEdits || def.AutoActivate {
		t.Fatalf("nil config settings = %+v", def)
	}
}

func TestLSPRunnerMode(t *testing.T) {
	cases := map[string]string{
		"":        LSPRunnerAuto,
		"  BUN ":  LSPRunnerBun,
		"npm":     LSPRunnerNpm,
		"Off":     LSPRunnerOff,
		"yarn":    LSPRunnerAuto,
		"auto":    LSPRunnerAuto,
		"pnpm@10": LSPRunnerAuto,
	}
	for value, want := range cases {
		if got := (&Config{LSPRunner: value}).LSPRunnerMode(); got != want {
			t.Errorf("LSPRunnerMode(%q) = %q, want %q", value, got, want)
		}
	}
	var nilCfg *Config
	if got := nilCfg.LSPRunnerMode(); got != LSPRunnerAuto {
		t.Errorf("nil config runner = %q", got)
	}
}

func TestLSPAutoInstallRequiresARunner(t *testing.T) {
	if !(&Config{LSPAutoInstall: true}).LSPAutoInstallEnabled() {
		t.Fatal("auto-install must be enabled by default")
	}
	// "off" wins over LSPAutoInstall: there is nothing left to install with.
	if (&Config{LSPAutoInstall: true, LSPRunner: LSPRunnerOff}).LSPAutoInstallEnabled() {
		t.Fatal(`LSPRunner = "off" must disable automatic installation`)
	}
}

func TestUpdateLSPActivationValidates(t *testing.T) {
	isolateGlobalConfig(t)
	cfg = nil
	viper.Reset()
	t.Cleanup(func() {
		cfg = nil
		viper.Reset()
	})
	if _, err := Load(t.TempDir(), false); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := UpdateLSPActivation(LSPActivationSettings{ActivateOn: "sometimes"}); err == nil {
		t.Fatal("an unknown mode must be rejected")
	}
	if err := UpdateLSPActivation(LSPActivationSettings{StartupTimeout: "soon"}); err == nil {
		t.Fatal("an unparsable timeout must be rejected")
	}
	if err := UpdateLSPActivation(LSPActivationSettings{StartupTimeout: "-5s"}); err == nil {
		t.Fatal("a non-positive timeout must be rejected")
	}
	if err := UpdateLSPActivation(LSPActivationSettings{Runner: "yarn"}); err == nil {
		t.Fatal("an unknown runner must be rejected")
	}

	err := UpdateLSPActivation(LSPActivationSettings{
		AutoActivate:   true,
		ActivateOn:     "  Reads ",
		AutoInstall:    true,
		Runner:         " NPM ",
		StartupTimeout: "30s",
	})
	if err != nil {
		t.Fatalf("UpdateLSPActivation: %v", err)
	}
	got := Get().LSPActivationSettings()
	if got.ActivateOn != LSPActivateReads || !got.AutoInstall || got.StartupTimeout != "30s" {
		t.Fatalf("settings = %+v", got)
	}
	if got.Runner != LSPRunnerNpm {
		t.Fatalf("runner = %q, want %q", got.Runner, LSPRunnerNpm)
	}
	// An empty timeout falls back to the documented default rather than to an
	// unparsable empty string.
	if got.InstallTimeout != "2m0s" {
		t.Fatalf("install timeout = %q", got.InstallTimeout)
	}
}

func TestLSPTimeoutHelpers(t *testing.T) {
	c := &Config{}
	if got := c.LSPStartupWait().String(); got != "20s" {
		t.Fatalf("default startup wait = %s", got)
	}
	if got := c.LSPInstallWait().String(); got != "2m0s" {
		t.Fatalf("default install wait = %s", got)
	}

	c = &Config{LSPStartupTimeout: "5s", LSPInstallTimeout: "1s"}
	// The install wait can never be shorter than the startup wait.
	if got := c.LSPInstallWait().String(); got != "5s" {
		t.Fatalf("install wait = %s, want it clamped to the startup wait", got)
	}

	c = &Config{LSPStartupTimeout: "garbage"}
	if got := c.LSPStartupWait().String(); got != "20s" {
		t.Fatalf("unparsable value must fall back to the default, got %s", got)
	}
}
