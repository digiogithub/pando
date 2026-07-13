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

func TestParseSuperpowersCommands(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs string
	}{
		{input: "/superpowers", wantName: "superpowers"},
		{input: "/superpowers port the indexer", wantName: "superpowers", wantArgs: "port the indexer"},
		{input: "/superpowers-finish", wantName: "superpowers-finish"},
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
