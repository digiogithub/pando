package sandbox

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

const (
	testHome = "/home/dev"
	testWS   = "/home/dev/src/app"
)

// testOpts resolves for Linux with a fixed home and the given environment.
func testOpts(env map[string]string, locked ...string) ResolveOptions {
	return ResolveOptions{
		GOOS:    "linux",
		HomeDir: testHome,
		LookupEnv: func(k string) (string, bool) {
			v, ok := env[k]
			return v, ok
		},
		IsLocked: func(path string) bool { return slices.Contains(locked, path) },
	}
}

func TestResolveEmptyConfigDefaults(t *testing.T) {
	p := ResolveConfig(config.SandboxConfig{}, testWS, testOpts(nil))

	if p.Mode != ModeWorkspaceWrite || p.Network != NetworkAllowed || !p.AutoAllowBash {
		t.Fatalf("defaults = mode %q network %q autoAllow %v, want workspace-write/allowed/true", p.Mode, p.Network, p.AutoAllowBash)
	}
	if !p.Enabled() || p.RestrictsNetwork() {
		t.Fatal("default policy must be enabled with the network open")
	}
	if p.Source != SourceDefault {
		t.Fatalf("Source = %q, want default", p.Source)
	}
	for _, want := range []string{testWS, "/tmp", "/var/tmp", testHome + "/.cache", testHome + "/go/pkg/mod",
		testHome + "/.npm", testHome + "/.cargo/registry", testHome + "/.bun/install/cache"} {
		if !slices.Contains(p.WritableRoots, want) {
			t.Errorf("WritableRoots missing %q: %v", want, p.WritableRoots)
		}
	}
	for _, want := range []string{testWS + "/.pando", testWS + "/.pando.toml", testWS + "/.git/hooks",
		testWS + "/.git/config", testHome + "/.pando.toml", testHome + "/.config/pando"} {
		if !slices.Contains(p.ProtectedPaths, want) {
			t.Errorf("ProtectedPaths missing %q: %v", want, p.ProtectedPaths)
		}
	}
	if len(p.ReadableRoots) != 0 {
		t.Fatalf("ReadableRoots = %v, want none (read everything)", p.ReadableRoots)
	}
	if p.Env.Inherit != EnvInheritAll || !p.Env.ScrubSecrets {
		t.Fatalf("Env = %+v, want inherit all + scrub secrets", p.Env)
	}
	if p.UseBwrap != BwrapAuto {
		t.Fatalf("UseBwrap = %q, want auto", p.UseBwrap)
	}
	if !p.Covers(PurposeBash) || !p.Covers(PurposeACPTerminals) || !p.Covers(PurposeSkills) || p.Covers(PurposeMCP) {
		t.Fatalf("ExtendTo = %v, want bash + default set only", p.ExtendTo)
	}
}

func TestResolveModes(t *testing.T) {
	extra := []string{"/opt/extra", "~/work", "rel"}

	t.Run("workspace-write", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Mode: "workspace-write", WritableRoots: extra}, testWS, testOpts(nil))
		for _, want := range []string{testWS, "/opt/extra", testHome + "/work", testWS + "/rel"} {
			if !slices.Contains(p.WritableRoots, want) {
				t.Errorf("WritableRoots missing %q: %v", want, p.WritableRoots)
			}
		}
	})

	t.Run("workspace-write restricted network without caches", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Network: "restricted", CacheDirsDisabled: true}, testWS, testOpts(nil))
		if !p.RestrictsNetwork() {
			t.Fatal("network not restricted")
		}
		if slices.Contains(p.WritableRoots, testHome+"/.cache") {
			t.Fatalf("cache dirs present although disabled: %v", p.WritableRoots)
		}
	})

	t.Run("read-only", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Mode: "read-only", WritableRoots: extra}, testWS, testOpts(map[string]string{"TMPDIR": "/scratch"}))
		if p.Mode != ModeReadOnly || !p.RestrictsNetwork() {
			t.Fatalf("mode %q network %q, want read-only/restricted", p.Mode, p.Network)
		}
		want := []string{"/scratch", "/tmp", "/var/tmp"}
		if !slices.Equal(p.WritableRoots, want) {
			t.Fatalf("WritableRoots = %v, want temp only %v", p.WritableRoots, want)
		}
	})

	t.Run("strict", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Mode: "Strict", ReadOnlyRoots: []string{"/data/ref"}}, testWS, testOpts(nil))
		if p.Mode != ModeStrict || !p.RestrictsNetwork() {
			t.Fatalf("mode %q network %q, want strict/restricted", p.Mode, p.Network)
		}
		if !slices.Contains(p.WritableRoots, testWS) || slices.Contains(p.WritableRoots, testHome+"/.cache") {
			t.Fatalf("WritableRoots = %v, want workspace+temp without caches", p.WritableRoots)
		}
		for _, want := range []string{testWS, "/usr", "/etc", "/tmp", "/data/ref"} {
			if !slices.Contains(p.ReadableRoots, want) {
				t.Errorf("ReadableRoots missing %q: %v", want, p.ReadableRoots)
			}
		}
	})

	t.Run("off via mode", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Mode: "off"}, testWS, testOpts(nil))
		if p.Enabled() || p.AutoAllowBash || len(p.WritableRoots) != 0 || p.Covers(PurposeBash) {
			t.Fatalf("off policy = %+v, want disabled and empty", p)
		}
	})

	t.Run("off via disabled", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Disabled: true, Mode: "strict"}, testWS, testOpts(nil))
		if p.Mode != ModeOff || p.Source != SourceConfig {
			t.Fatalf("mode %q source %q, want off/config", p.Mode, p.Source)
		}
	})

	t.Run("unknown mode falls back on", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{Mode: "yolo"}, testWS, testOpts(nil))
		if p.Mode != ModeWorkspaceWrite {
			t.Fatalf("mode = %q, want workspace-write", p.Mode)
		}
	})

	t.Run("auto-allow disabled", func(t *testing.T) {
		p := ResolveConfig(config.SandboxConfig{AutoAllowBashDisabled: true}, testWS, testOpts(nil))
		if p.AutoAllowBash {
			t.Fatal("AutoAllowBash = true, want false")
		}
	})
}

func TestResolvePrecedence(t *testing.T) {
	strict := config.SandboxConfig{Mode: "strict"}

	cases := []struct {
		name       string
		cfg        config.SandboxConfig
		env        map[string]string
		locked     []string
		wantMode   Mode
		wantSource Source
	}{
		{"config over default", strict, nil, nil, ModeStrict, SourceConfig},
		{"env over config", strict, map[string]string{EnvVar: "read-only"}, nil, ModeReadOnly, SourceEnv},
		{"env off over config", strict, map[string]string{EnvVar: "off"}, nil, ModeOff, SourceEnv},
		{"env false means off", config.SandboxConfig{}, map[string]string{EnvVar: "false"}, nil, ModeOff, SourceEnv},
		{"env on re-enables configured mode", config.SandboxConfig{Disabled: true, Mode: "strict"}, map[string]string{EnvVar: "on"}, nil, ModeStrict, SourceEnv},
		{"env on without mode", config.SandboxConfig{Disabled: true}, map[string]string{EnvVar: "1"}, nil, ModeWorkspaceWrite, SourceEnv},
		{"invalid env ignored", strict, map[string]string{EnvVar: "sometimes"}, nil, ModeStrict, SourceConfig},
		{"empty env ignored", strict, map[string]string{EnvVar: " "}, nil, ModeStrict, SourceConfig},
		{"lock on mode beats env", strict, map[string]string{EnvVar: "off"}, []string{"sandbox.mode"}, ModeStrict, SourceLock},
		{"lock on disabled beats env", config.SandboxConfig{}, map[string]string{EnvVar: "off"}, []string{"sandbox.disabled"}, ModeWorkspaceWrite, SourceLock},
		{"unrelated lock does not block env", strict, map[string]string{EnvVar: "off"}, []string{"sandbox.network"}, ModeOff, SourceEnv},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := ResolveConfig(tc.cfg, testWS, testOpts(tc.env, tc.locked...))
			if p.Mode != tc.wantMode || p.Source != tc.wantSource {
				t.Fatalf("mode %q source %q, want %q/%q", p.Mode, p.Source, tc.wantMode, tc.wantSource)
			}
		})
	}
}

func TestResolvePathsPerOS(t *testing.T) {
	t.Run("darwin temp aliases and caches", func(t *testing.T) {
		opts := testOpts(nil)
		opts.GOOS = "darwin"
		opts.HomeDir = "/Users/dev"
		p := ResolveConfig(config.SandboxConfig{}, "/Users/dev/app", opts)
		for _, want := range []string{"/private/tmp", "/private/var/folders", "/Users/dev/Library/Caches"} {
			if !slices.Contains(p.WritableRoots, want) {
				t.Errorf("WritableRoots missing %q: %v", want, p.WritableRoots)
			}
		}
	})

	t.Run("env cache overrides", func(t *testing.T) {
		env := map[string]string{"GOMODCACHE": "/cache/gomod", "GOPATH": "/gp:/other", "CARGO_HOME": "/cargo", "GOCACHE": "relative/ignored"}
		p := ResolveConfig(config.SandboxConfig{}, testWS, testOpts(env))
		for _, want := range []string{"/cache/gomod", "/gp/pkg/mod", "/cargo/registry", "/cargo/git"} {
			if !slices.Contains(p.WritableRoots, want) {
				t.Errorf("WritableRoots missing %q: %v", want, p.WritableRoots)
			}
		}
		if slices.Contains(p.WritableRoots, testHome+"/go/pkg/mod") {
			t.Errorf("default GOPATH module cache present although GOPATH is set: %v", p.WritableRoots)
		}
	})

	t.Run("protected data dir and local config", func(t *testing.T) {
		opts := testOpts(map[string]string{"XDG_CONFIG_HOME": "/xdg"})
		opts.DataDir = "/var/pando-data"
		opts.LocalConfigFile = "/home/dev/src/.pando.json"
		p := ResolveConfig(config.SandboxConfig{DenyPaths: []string{"~/.ssh", "**/.env"}}, testWS, opts)
		for _, want := range []string{"/var/pando-data", "/home/dev/src/.pando.json", "/xdg/pando"} {
			if !slices.Contains(p.ProtectedPaths, want) {
				t.Errorf("ProtectedPaths missing %q: %v", want, p.ProtectedPaths)
			}
		}
		for _, want := range []string{testHome + "/.ssh", filepath.Join(testWS, "**/.env")} {
			if !slices.Contains(p.DenyPaths, want) {
				t.Errorf("DenyPaths missing %q: %v", want, p.DenyPaths)
			}
		}
	})
}

func TestPolicyHashStability(t *testing.T) {
	base := ResolveConfig(config.SandboxConfig{WritableRoots: []string{"/a", "/b"}}, testWS, testOpts(nil))

	again := ResolveConfig(config.SandboxConfig{WritableRoots: []string{"/b", "/a", "/a"}}, testWS, testOpts(nil))
	if base.Hash() != again.Hash() {
		t.Fatal("hash depends on root order or duplicates")
	}
	if base.Hash() != base.Hash() || len(base.Hash()) != 64 {
		t.Fatalf("hash not deterministic or not sha256 hex: %q", base.Hash())
	}

	shuffled := base
	shuffled.WritableRoots = slices.Clone(base.WritableRoots)
	slices.Reverse(shuffled.WritableRoots)
	shuffled.Source = SourceEnv
	if shuffled.Hash() != base.Hash() {
		t.Fatal("hash depends on slice order or on Source")
	}

	for name, other := range map[string]config.SandboxConfig{
		"mode":      {Mode: "strict", WritableRoots: []string{"/a", "/b"}},
		"network":   {Network: "restricted", WritableRoots: []string{"/a", "/b"}},
		"roots":     {WritableRoots: []string{"/a"}},
		"autoallow": {AutoAllowBashDisabled: true, WritableRoots: []string{"/a", "/b"}},
		"env":       {Env: config.SandboxEnvConfig{Exclude: []string{"X"}}, WritableRoots: []string{"/a", "/b"}},
		"off":       {Disabled: true},
	} {
		if ResolveConfig(other, testWS, testOpts(nil)).Hash() == base.Hash() {
			t.Errorf("changing %s did not change the hash", name)
		}
	}
	if ResolveConfig(config.SandboxConfig{WritableRoots: []string{"/a", "/b"}}, "/elsewhere", testOpts(nil)).Hash() == base.Hash() {
		t.Error("changing the workspace did not change the hash")
	}
}

type fakeWrapper struct {
	enforced bool
	wrapped  int
}

func (f *fakeWrapper) Capability() Capability {
	return Capability{Backend: "fake", Enforced: f.enforced, ProtectsNestedPaths: true, BlocksPorts: true}
}

func (f *fakeWrapper) Wrap(cmd *exec.Cmd, p Policy) error {
	f.wrapped++
	cmd.Args = append([]string{"wrapped"}, cmd.Args...)
	return nil
}

func TestCurrentActiveAndWrapCmd(t *testing.T) {
	ws := t.TempDir()
	t.Setenv(EnvVar, "")
	config.SetForTests(&config.Config{WorkingDir: ws})
	t.Cleanup(func() { config.SetForTests(nil) })

	fake := &fakeWrapper{}
	restore := SetDefaultForTests(fake)
	t.Cleanup(restore)

	if Current().Workspace != ws || CurrentPolicyHash() != Current().Hash() {
		t.Fatal("Current does not follow the live config")
	}
	if Active() || AutoAllowBash() {
		t.Fatal("not-enforced backend reported active / auto-allow")
	}
	fake.enforced = true
	if !Active() || !AutoAllowBash() {
		t.Fatal("enforced backend with default policy must be active with auto-allow")
	}
	if got := CurrentStatus().Label(); got != "workspace-write (fake)" {
		t.Fatalf("Label = %q", got)
	}

	cmd := exec.Command("sh", "-c", "true")
	cmd.Env = []string{"PATH=/bin", "OPENAI_API_KEY=sk"}
	p, c, err := WrapCmd(cmd, PurposeBash)
	if err != nil || !c.Enforced || !p.Enabled() {
		t.Fatalf("WrapCmd = %v, %+v, %v", p.Mode, c, err)
	}
	if fake.wrapped != 1 || cmd.Args[0] != "wrapped" || !slices.Equal(cmd.Env, []string{"PATH=/bin"}) {
		t.Fatalf("cmd not wrapped/scrubbed: args %v env %v", cmd.Args, cmd.Env)
	}

	mcp := exec.Command("server")
	if _, _, err := WrapCmd(mcp, PurposeMCP); err != nil || fake.wrapped != 1 || mcp.Env != nil {
		t.Fatal("WrapCmd touched a purpose the policy does not cover")
	}

	t.Setenv(EnvVar, "off")
	if Active() || AutoAllowBash() || CurrentStatus().Label() != "off" {
		t.Fatal("PANDO_SANDBOX=off not reflected by the live accessors")
	}
}

func TestDefaultWrapperIsUsable(t *testing.T) {
	w := Default()
	if w == nil || w.Capability().Backend == "" {
		t.Fatalf("Default() = %#v", w)
	}
	if w != Default() {
		t.Fatal("Default() is not memoised")
	}
	cmd := exec.Command("true")
	if err := w.Wrap(cmd, Policy{Mode: ModeOff}); err != nil {
		t.Fatalf("Wrap(off) = %v", err)
	}
	if cmd.Path == "" || len(cmd.Args) != 1 {
		t.Fatalf("Wrap(off) modified the command: %v", cmd.Args)
	}
}

func TestNoopWrapperNotEnforced(t *testing.T) {
	w := newNoopWrapper("test: nope")
	c := w.Capability()
	if c.Enforced || c.Backend != BackendNone || c.Reason != "test: nope" {
		t.Fatalf("Capability = %+v", c)
	}
	cmd := exec.Command("true")
	args := slices.Clone(cmd.Args)
	if err := w.Wrap(cmd, Policy{Mode: ModeStrict}); err != nil || !slices.Equal(cmd.Args, args) {
		t.Fatalf("noop Wrap changed the command or failed: %v %v", cmd.Args, err)
	}
	s := Status{Policy: Policy{Mode: ModeStrict}, Capability: c}
	if got := s.Label(); got != "strict (not enforced: test: nope)" {
		t.Fatalf("Label = %q", got)
	}
}
