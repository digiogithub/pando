package dialog

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/tui/util"
)

// LoadExtensionCommands turns the slash commands contributed by loaded
// extensions into palette entries.
//
// They are built here rather than in the extension system because the palette
// is a TUI concept: the extension only declares a name, a description and what
// running it produced, and each surface decides how to show that.
func LoadExtensionCommands() []Command {
	var out []Command
	for _, sc := range commands.ExtensionCommands() {
		out = append(out, Command{
			ID:          sc.Name,
			Title:       sc.Name,
			Description: sc.Description,
			Category:    CommandCategoryExtensions,
			Handler:     extensionCommandHandler(sc.Name),
		})
	}
	return out
}

// extensionCommandHandler runs one extension command and maps its result onto
// the TUI: Output becomes an info report, Prompt is sent to the agent as if the
// user had typed it, and neither means the command only changed hidden state.
//
// The palette cannot collect arguments, so commands invoked from it always run
// with an empty argument string; typing "/name args" in the editor is the way
// to pass them.
func extensionCommandHandler(name string) func(Command) tea.Cmd {
	return func(Command) tea.Cmd {
		return func() tea.Msg {
			res, handled, err := commands.RunExtension(context.Background(), name, "")
			if err != nil {
				return util.ReportError(fmt.Errorf("/%s: %w", name, err))()
			}
			if !handled {
				return util.ReportError(fmt.Errorf("/%s is no longer available", name))()
			}
			if strings.TrimSpace(res.Prompt) != "" {
				return CommandRunCustomMsg{Content: res.Prompt}
			}
			if res.Output != "" {
				return util.ReportInfo(res.Output)()
			}
			return nil
		}
	}
}
