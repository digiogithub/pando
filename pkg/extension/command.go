package extension

import "context"

// This file declares the two user-facing command surfaces an extension can
// contribute: CLI subcommands of the `pando` binary, and slash commands typed
// inside a session.
//
// Neither contract mentions cobra. An out-of-tree module must not have to agree
// with core on a third-party dependency version just to add a subcommand, so
// the shape below is stdlib-only and core adapts it (internal/extensions).

// Flag declares one command-line flag. Value carries both the default and the
// type: bool, string, int and []string are supported, and anything else is
// rejected when the command is registered.
type Flag struct {
	// Name is the long flag name, without dashes.
	Name string
	// Shorthand is the optional one-letter form, without the dash.
	Shorthand string
	// Usage is the one-line help text.
	Usage string
	// Value is the default value and, by its dynamic type, the flag type.
	Value any
}

// Flags is the parsed flag set handed to a command at run time. Missing keys
// mean the flag was never declared; a declared flag is always present, holding
// its default when the user did not pass it.
type Flags map[string]any

// Bool reads a boolean flag.
func (f Flags) Bool(name string) bool {
	v, _ := f[name].(bool)
	return v
}

// String reads a string flag.
func (f Flags) String(name string) string {
	v, _ := f[name].(string)
	return v
}

// Int reads an integer flag. Both int and int64 are accepted so the same
// accessor works whichever numeric type the host's flag library produced.
func (f Flags) Int(name string) int {
	switch v := f[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// StringSlice reads a repeated string flag.
func (f Flags) StringSlice(name string) []string {
	v, _ := f[name].([]string)
	return v
}

// Command is one CLI subcommand of the pando binary.
//
// Commands are mounted under the `ext` subcommand, never at the top level:
// `pando ext <use>`. That is deliberate — an extension cannot shadow a core
// command, and `pando ext --help` lists exactly what the build added.
type Command struct {
	// Use is the command name, optionally followed by its argument sketch
	// ("sync [target]"). The first word must be unique among all extension
	// commands in the build.
	Use string
	// Short is the one-line description shown in the command list.
	Short string
	// Long is the full help text.
	Long string
	// Aliases are alternative names for the command.
	Aliases []string
	// Flags declares the command's flags.
	Flags []Flag
	// Run executes the command. args holds the positional arguments after the
	// command name. Returning an error makes the process exit non-zero with
	// that message.
	Run func(ctx context.Context, args []string, flags Flags) error
	// Subcommands nest below this one. Their Use names must be unique among
	// their siblings.
	Subcommands []Command
}

// CommandProvider is implemented by extensions that add CLI subcommands.
//
// Commands are collected before the manager provisions anything, because the
// CLI must be able to print help without starting Pando. An extension whose
// commands depend on provisioned state should do that work inside Run, not
// while building the Command value.
type CommandProvider interface {
	Extension
	Commands() []Command
}

// SlashCommand is one command a user can type in a session as "/name args".
type SlashCommand struct {
	// Name is typed after the slash, lowercase, no spaces. It must not collide
	// with a built-in command; registration rejects the collision rather than
	// letting an extension hijack /compact.
	Name string
	// Description is shown in the command palette and completions.
	Description string
	// AcceptsArgs tells the UI whether to expect text after the name.
	AcceptsArgs bool
}

// SlashResult is what running a slash command produced. Exactly one of Prompt
// and Output is normally set:
//
//   - Prompt is sent to the model as if the user had typed it, which is how
//     prompt-expanding commands (the /vulnhunt family, custom .md commands)
//     work.
//   - Output is shown to the user directly and starts no model turn, which is
//     how state-changing commands (/caveman, /goal-status) work.
//
// Setting both shows Output and then runs Prompt. Setting neither is a no-op,
// which is the right result for a command that only changed hidden state.
type SlashResult struct {
	Prompt string
	Output string
}

// SlashCommandProvider is implemented by extensions that add slash commands.
// The same extension must both declare and execute them: core routes by name.
type SlashCommandProvider interface {
	Extension
	SlashCommands() []SlashCommand
	// RunSlashCommand executes one of the declared commands. args is the raw
	// text after the command name, untrimmed of internal spacing. Returning an
	// error surfaces it to the user; it does not abort the session.
	RunSlashCommand(ctx context.Context, name, args string) (SlashResult, error)
}
