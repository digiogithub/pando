package completions

import (
	"fmt"
	"os"

	"github.com/digiogithub/pando/internal/fileutil"
	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/tui/components/dialog"
)

// maxCompletionFiles is the maximum number of files returned by the file
// completion provider.  Keeping this small avoids UI freezes in large trees.
const maxCompletionFiles = 500

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
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	// Skip recursive glob in non-project directories (home, root, …) to
	// avoid freezing the TUI by scanning hundreds of GB.
	if !fileutil.IsSafeWorkingDirectory(cwd) {
		logging.Debug("file completions: skipping glob – not a project directory", "cwd", cwd)
		return nil, nil
	}

	logging.Debug("Using doublestar for file listing")
	files, _, err := fileutil.GlobWithDoublestar("**/*", ".", maxCompletionFiles)
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
