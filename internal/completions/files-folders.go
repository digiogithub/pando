package completions

import (
	"fmt"

	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
)

type filesAndFoldersContextGroup struct {
	prefix string
}

func (cg *filesAndFoldersContextGroup) GetId() string {
	return cg.prefix
}

func (cg *filesAndFoldersContextGroup) GetEntry() dialog.CompletionItemI {
	return dialog.NewCompletionItem(dialog.CompletionItem{
		Title: "Files & Folders",
		Value: "files",
	})
}

func (cg *filesAndFoldersContextGroup) getFiles(query string) ([]string, error) {
	logging.Debug("Using doublestar for file listing")
	files, _, err := fileutil.GlobWithDoublestar("**/*", ".", 0)
	if err != nil {
		return nil, fmt.Errorf("failed to glob files: %w", err)
	}

	allFiles := make([]string, 0, len(files))
	for _, file := range files {
		if !fileutil.SkipHidden(file) {
			allFiles = append(allFiles, file)
		}
	}

	return fileutil.FuzzyFilter(query, allFiles), nil
}

func (cg *filesAndFoldersContextGroup) GetChildEntries(query string) ([]dialog.CompletionItemI, error) {
	matches, err := cg.getFiles(query)
	if err != nil {
		return nil, err
	}

	items := make([]dialog.CompletionItemI, 0, len(matches))
	for _, file := range matches {
		item := dialog.NewCompletionItem(dialog.CompletionItem{
			Title: file,
			Value: file,
		})
		items = append(items, item)
	}

	return items, nil
}

func NewFileAndFolderContextGroup() dialog.CompletionProvider {
	return &filesAndFoldersContextGroup{
		prefix: "file",
	}
}
