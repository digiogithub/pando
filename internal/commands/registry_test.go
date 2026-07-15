package commands

import "testing"

func TestBuiltinCommandsIncludeSuperpowers(t *testing.T) {
	byName := map[string]SlashCommand{}
	for _, cmd := range BuiltinCommands() {
		byName[cmd.Name] = cmd
	}

	superpowers, ok := byName["superpowers"]
	if !ok {
		t.Fatal("expected /superpowers to be a built-in command")
	}
	if !superpowers.AcceptsArgs {
		t.Error("expected /superpowers to accept an optional objective")
	}

	finish, ok := byName["superpowers-finish"]
	if !ok {
		t.Fatal("expected /superpowers-finish to be a built-in command")
	}
	if finish.AcceptsArgs {
		t.Error("expected /superpowers-finish to take no arguments")
	}
}

func TestBuiltinCommandsIncludeLearning(t *testing.T) {
	byName := map[string]SlashCommand{}
	for _, cmd := range BuiltinCommands() {
		byName[cmd.Name] = cmd
	}

	learning, ok := byName["learning"]
	if !ok {
		t.Fatal("expected /learning to be a built-in command")
	}
	if !learning.AcceptsArgs {
		t.Error("expected /learning to accept an optional focus")
	}

	finish, ok := byName["learning-finish"]
	if !ok {
		t.Fatal("expected /learning-finish to be a built-in command")
	}
	if finish.AcceptsArgs {
		t.Error("expected /learning-finish to take no arguments")
	}
}

func TestBuiltinCommandsIncludeCaveman(t *testing.T) {
	byName := map[string]SlashCommand{}
	for _, cmd := range BuiltinCommands() {
		byName[cmd.Name] = cmd
	}

	cm, ok := byName["caveman"]
	if !ok {
		t.Fatal("expected /caveman to be a built-in command")
	}
	if !cm.AcceptsArgs {
		t.Error("expected /caveman to accept a level argument")
	}

	finish, ok := byName["caveman-finish"]
	if !ok {
		t.Fatal("expected /caveman-finish to be a built-in command")
	}
	if finish.AcceptsArgs {
		t.Error("expected /caveman-finish to take no arguments")
	}
}

func TestParseCavemanCommands(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs string
	}{
		{input: "/caveman", wantName: "caveman"},
		{input: "/caveman ultra", wantName: "caveman", wantArgs: "ultra"},
		{input: "/caveman-finish", wantName: "caveman-finish"},
	}

	for _, tt := range tests {
		name, args, ok := Parse(tt.input)
		if !ok {
			t.Fatalf("expected %q to parse as a slash command", tt.input)
		}
		if name != tt.wantName {
			t.Errorf("Parse(%q) name = %q, want %q", tt.input, name, tt.wantName)
		}
		if args != tt.wantArgs {
			t.Errorf("Parse(%q) args = %q, want %q", tt.input, args, tt.wantArgs)
		}
	}
}

func TestParseSuperpowersCommands(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs string
	}{
		{input: "/superpowers", wantName: "superpowers"},
		{input: "/superpowers port the indexer", wantName: "superpowers", wantArgs: "port the indexer"},
		{input: "/superpowers-finish", wantName: "superpowers-finish"},
		{input: "/learning", wantName: "learning"},
		{input: "/learning understand the KB graph", wantName: "learning", wantArgs: "understand the KB graph"},
		{input: "/learning-finish", wantName: "learning-finish"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, args, ok := Parse(tt.input)
			if !ok {
				t.Fatalf("expected %q to parse", tt.input)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if args != tt.wantArgs {
				t.Errorf("args = %q, want %q", args, tt.wantArgs)
			}
		})
	}
}

// Match drives slash-command completion in the TUI and Web UI: typing "/super"
// must offer both commands, and the finish command must not shadow activation.
func TestMatchOffersBothSuperpowersCommands(t *testing.T) {
	matched := Match("super", BuiltinCommands())
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches for %q, got %d", "super", len(matched))
	}
}
