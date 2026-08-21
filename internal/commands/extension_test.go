package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/digiogithub/pando/pkg/extension"
)

type slashExt struct {
	id    extension.ID
	cmds  []extension.SlashCommand
	run   func(ctx context.Context, name, args string) (extension.SlashResult, error)
	calls []string
}

func (e *slashExt) ExtensionInfo() extension.Info {
	return extension.Info{ID: e.id, Version: "1.0.0", New: func() extension.Extension { return e }}
}

func (e *slashExt) SlashCommands() []extension.SlashCommand { return e.cmds }

func (e *slashExt) RunSlashCommand(ctx context.Context, name, args string) (extension.SlashResult, error) {
	e.calls = append(e.calls, name+"|"+args)
	return e.run(ctx, name, args)
}

// wire loads exts into an isolated manager and attaches it for the duration of
// the test, so the package-level registry and manager are never touched.
func wire(t *testing.T, exts ...extension.Extension) {
	t.Helper()
	reg := extension.NewRegistry()
	for _, e := range exts {
		reg.Register(e)
	}
	mgr := extension.NewManager(extension.Options{Registry: reg})
	if err := mgr.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	SetExtensionManager(mgr)
	t.Cleanup(func() {
		SetExtensionManager(nil)
		mgr.Cleanup()
	})
}

func TestNoManagerMeansNoExtensionCommands(t *testing.T) {
	SetExtensionManager(nil)
	if got := ExtensionCommands(); len(got) != 0 {
		t.Errorf("got %v", got)
	}
	if _, handled, _ := RunExtension(context.Background(), "anything", ""); handled {
		t.Error("RunExtension claimed a command with no manager")
	}
}

func TestExtensionCommandsAreListedAndParsed(t *testing.T) {
	wire(t, &slashExt{
		id:   "tools.acme",
		cmds: []extension.SlashCommand{{Name: "acme-sync", Description: "Sync with Acme", AcceptsArgs: true}},
		run: func(context.Context, string, string) (extension.SlashResult, error) {
			return extension.SlashResult{Output: "synced"}, nil
		},
	})

	found := false
	for _, c := range AllCommands("") {
		if c.Name == "acme-sync" {
			found = true
			if !c.AcceptsArgs || c.Description != "Sync with Acme" {
				t.Errorf("command not carried through: %+v", c)
			}
		}
	}
	if !found {
		t.Fatal("extension command missing from AllCommands")
	}

	name, args, ok := Parse("/acme-sync now")
	if !ok || name != "acme-sync" || args != "now" {
		t.Errorf("Parse = %q %q %v", name, args, ok)
	}
}

// An extension must not be able to redefine a built-in: /compact stays core's.
func TestBuiltinCommandsWinCollisions(t *testing.T) {
	ext := &slashExt{
		id:   "tools.acme",
		cmds: []extension.SlashCommand{{Name: "compact", Description: "hijack"}},
		run: func(context.Context, string, string) (extension.SlashResult, error) {
			return extension.SlashResult{Output: "hijacked"}, nil
		},
	}
	wire(t, ext)

	for _, c := range ExtensionCommands() {
		if c.Name == "compact" {
			t.Fatal("extension was allowed to redefine /compact")
		}
	}
	if _, handled, _ := RunExtension(context.Background(), "compact", ""); handled {
		t.Fatal("extension ran a built-in command")
	}
	if len(ext.calls) != 0 {
		t.Errorf("extension was called: %v", ext.calls)
	}
}

func TestRunExtensionRoutesToOwner(t *testing.T) {
	other := &slashExt{
		id:   "tools.other",
		cmds: []extension.SlashCommand{{Name: "other-cmd"}},
		run: func(context.Context, string, string) (extension.SlashResult, error) {
			return extension.SlashResult{Output: "wrong"}, nil
		},
	}
	owner := &slashExt{
		id:   "tools.acme",
		cmds: []extension.SlashCommand{{Name: "acme-review"}},
		run: func(_ context.Context, name, args string) (extension.SlashResult, error) {
			return extension.SlashResult{Prompt: "review " + args}, nil
		},
	}
	wire(t, other, owner)

	res, handled, err := RunExtension(context.Background(), "acme-review", "the diff")
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if res.Prompt != "review the diff" {
		t.Errorf("prompt = %q", res.Prompt)
	}
	if len(other.calls) != 0 {
		t.Errorf("wrong extension was called: %v", other.calls)
	}
}

func TestRunExtensionUnknownNameIsNotHandled(t *testing.T) {
	wire(t, &slashExt{
		id:   "tools.acme",
		cmds: []extension.SlashCommand{{Name: "acme-sync"}},
		run: func(context.Context, string, string) (extension.SlashResult, error) {
			return extension.SlashResult{}, nil
		},
	})
	if _, handled, _ := RunExtension(context.Background(), "not-a-command", ""); handled {
		t.Error("unknown command was claimed")
	}
}

// A broken command must surface as an error, not kill the session it was typed
// into.
func TestRunExtensionContainsPanic(t *testing.T) {
	wire(t, &slashExt{
		id:   "tools.bad",
		cmds: []extension.SlashCommand{{Name: "bad-cmd"}},
		run: func(context.Context, string, string) (extension.SlashResult, error) {
			panic("boom")
		},
	})

	_, handled, err := RunExtension(context.Background(), "bad-cmd", "")
	if !handled {
		t.Fatal("panicking command was not claimed")
	}
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v", err)
	}
}
