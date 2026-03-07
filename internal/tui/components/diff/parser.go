package diff

import (
	"regexp"
	"strconv"
	"strings"
)

// DiffLineType identifies the type of a diff line.
type DiffLineType int

const (
	DiffLineContext DiffLineType = iota
	DiffLineAdd
	DiffLineDelete
)

// DiffLine represents a single parsed line in a unified diff.
type DiffLine struct {
	OldNum  int
	NewNum  int
	Content string
	Type    DiffLineType
}

// Hunk represents a unified diff hunk.
type Hunk struct {
	Header             string
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []DiffLine
}

// FileDiff contains a parsed unified diff for a single file.
type FileDiff struct {
	OldFile   string
	NewFile   string
	FilePath  string
	Hunks     []Hunk
	Additions int
	Deletions int
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParseUnifiedDiff parses a unified diff into a FileDiff structure.
func ParseUnifiedDiff(diff string) *FileDiff {
	result := &FileDiff{}
	diff = strings.ReplaceAll(diff, "\r\n", "\n")
	if diff == "" {
		return result
	}

	lines := strings.Split(diff, "\n")
	var current *Hunk
	var oldLine, newLine int

	for idx, line := range lines {
		switch {
		case strings.HasPrefix(line, "--- "):
			result.OldFile = normalizeDiffPath(strings.TrimPrefix(line, "--- "))
			if result.FilePath == "" {
				result.FilePath = result.OldFile
			}
			continue
		case strings.HasPrefix(line, "+++ "):
			result.NewFile = normalizeDiffPath(strings.TrimPrefix(line, "+++ "))
			if result.NewFile != "" && result.NewFile != "/dev/null" {
				result.FilePath = result.NewFile
			}
			continue
		case strings.HasPrefix(line, "\\ No newline at end of file"):
			continue
		}

		if matches := hunkHeaderPattern.FindStringSubmatch(line); matches != nil {
			if current != nil {
				result.Hunks = append(result.Hunks, *current)
			}

			current = &Hunk{
				Header:   line,
				OldStart: parseDiffNumber(matches[1]),
				OldLines: parseDiffCount(matches[2]),
				NewStart: parseDiffNumber(matches[3]),
				NewLines: parseDiffCount(matches[4]),
				Lines:    make([]DiffLine, 0, 16),
			}
			oldLine = current.OldStart
			newLine = current.NewStart
			continue
		}

		if current == nil {
			continue
		}

		if line == "" && idx == len(lines)-1 {
			continue
		}

		diffLine := DiffLine{}
		switch {
		case strings.HasPrefix(line, "+"):
			diffLine = DiffLine{
				OldNum:  0,
				NewNum:  newLine,
				Content: strings.TrimPrefix(line, "+"),
				Type:    DiffLineAdd,
			}
			newLine++
			result.Additions++
		case strings.HasPrefix(line, "-"):
			diffLine = DiffLine{
				OldNum:  oldLine,
				NewNum:  0,
				Content: strings.TrimPrefix(line, "-"),
				Type:    DiffLineDelete,
			}
			oldLine++
			result.Deletions++
		case strings.HasPrefix(line, " "):
			diffLine = DiffLine{
				OldNum:  oldLine,
				NewNum:  newLine,
				Content: strings.TrimPrefix(line, " "),
				Type:    DiffLineContext,
			}
			oldLine++
			newLine++
		default:
			diffLine = DiffLine{
				OldNum:  oldLine,
				NewNum:  newLine,
				Content: line,
				Type:    DiffLineContext,
			}
			oldLine++
			newLine++
		}

		current.Lines = append(current.Lines, diffLine)
	}

	if current != nil {
		result.Hunks = append(result.Hunks, *current)
	}

	if result.FilePath == "" {
		if result.NewFile != "" {
			result.FilePath = result.NewFile
		} else {
			result.FilePath = result.OldFile
		}
	}

	return result
}

func normalizeDiffPath(path string) string {
	switch {
	case strings.HasPrefix(path, "a/"), strings.HasPrefix(path, "b/"):
		return path[2:]
	default:
		return path
	}
}

func parseDiffNumber(value string) int {
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return number
}

func parseDiffCount(value string) int {
	if value == "" {
		return 1
	}
	return parseDiffNumber(value)
}
