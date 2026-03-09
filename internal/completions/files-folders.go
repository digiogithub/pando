package completions

import (
	"bytes"
	"fmt"
	"path/filepath"

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

func processNullTerminatedOutput(outputBytes []byte) []string {
	if len(outputBytes) > 0 && outputBytes[len(outputBytes)-1] == 0 {
		outputBytes = outputBytes[:len(outputBytes)-1]
	}

	if len(outputBytes) == 0 {
		return []string{}
	}

	split := bytes.Split(outputBytes, []byte{0})
	matches := make([]string, 0, len(split))

	for _, p := range split {
		if len(p) == 0 {
			continue
		}

		path := string(p)
		path = filepath.Join(".", path)

		if !fileutil.SkipHidden(path) {
			matches = append(matches, path)
		}
	}

	return matches
}

func (cg *filesAndFoldersContextGroup) getFiles(query string) ([]string, error) {
	cmdRg := fileutil.GetRgCmd("")

	var allFiles []string

	if cmdRg != nil {
		logging.Debug("Using Ripgrep for file listing")
		var rgOut bytes.Buffer
		var rgErr bytes.Buffer
		cmdRg.Stdout = &rgOut
		cmdRg.Stderr = &rgErr

		if err := cmdRg.Run(); err != nil {
			return nil, fmt.Errorf("rg command failed: %w\nStderr: %s", err, rgErr.String())
		}

		allFiles = processNullTerminatedOutput(rgOut.Bytes())
	} else {
		logging.Debug("Using doublestar for file listing")
		files, _, err := fileutil.GlobWithDoublestar("**/*", ".", 0)
		if err != nil {
			return nil, fmt.Errorf("failed to glob files: %w", err)
		}

		allFiles = make([]string, 0, len(files))
		for _, file := range files {
			if !fileutil.SkipHidden(file) {
				allFiles = append(allFiles, file)
			}
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
