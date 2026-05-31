package completions

import (
	"fmt"

	"github.com/digiogithub/pando/internal/commands"
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
)

type slashCommandsProvider struct{}

func (p *slashCommandsProvider) GetId() string {
	return "/"
}

func (p *slashCommandsProvider) GetEntry() dialog.CompletionItemI {
	return dialog.NewCompletionItem(dialog.CompletionItem{
		Title: "Commands",
		Value: "/",
	})
}

func (p *slashCommandsProvider) GetChildEntries(query string) ([]dialog.CompletionItemI, error) {
	dataDir := ""
	if cfg := config.Get(); cfg != nil {
		dataDir = cfg.Data.Directory
	}
	allCmds := commands.AllCommands(dataDir)
	filtered := commands.Match(query, allCmds)

	items := make([]dialog.CompletionItemI, 0, len(filtered))
	for _, cmd := range filtered {
		item := dialog.NewCompletionItem(dialog.CompletionItem{
			Title: fmt.Sprintf("/%s — %s", cmd.Name, cmd.Description),
			Value: "/" + cmd.Name,
		})
		items = append(items, item)
	}
	return items, nil
}

// NewSlashCommandsProvider returns a CompletionProvider for slash commands.
func NewSlashCommandsProvider() dialog.CompletionProvider {
	return &slashCommandsProvider{}
}
