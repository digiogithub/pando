package search

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"regexp"
)

// OutputMode controls what the searcher returns.
type OutputMode string

const (
	OutputModeContent OutputMode = "content"            // matched lines + context
	OutputModeFiles   OutputMode = "files_with_matches" // just file paths
	OutputModeCount   OutputMode = "count"              // count per file
)

// LineMatch is a single matched or context line from a search.
type LineMatch struct {
	LineNum   int
	Text      string
	IsContext bool // true = context line, false = actual match
}

// SearchOptions configures a content search operation.
type SearchOptions struct {
	Pattern    *regexp.Regexp
	OutputMode OutputMode
	BeforeCtx  int  // lines of before-context (-B)
	AfterCtx   int  // lines of after-context (-A)
	Multiline  bool // match across lines (load whole file)
}

const binaryCheckSize = 4096

// isBinaryFile checks whether a file appears to be binary by scanning the first
// binaryCheckSize bytes for null bytes (same heuristic as ripgrep).
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, binaryCheckSize)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return false
	}
	return bytes.IndexByte(buf[:n], 0) != -1
}

// SearchFile searches a single file and returns matched lines.
// Returns nil, nil if the file is binary or has no matches.
func SearchFile(ctx context.Context, path string, opts SearchOptions) ([]LineMatch, error) {
	if isBinaryFile(path) {
		return nil, nil
	}

	if opts.Multiline {
		return searchMultiline(ctx, path, opts)
	}
	return searchLineByLine(ctx, path, opts)
}

func searchLineByLine(ctx context.Context, path string, opts SearchOptions) ([]LineMatch, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var results []LineMatch
	// Circular buffer for before-context
	beforeBuf := make([]string, 0, opts.BeforeCtx)
	beforeLineNums := make([]int, 0, opts.BeforeCtx)
	afterRemaining := 0
	lineNum := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		lineNum++
		line := scanner.Text()

		if opts.Pattern.MatchString(line) {
			// Emit before-context lines
			for i, bLine := range beforeBuf {
				results = append(results, LineMatch{
					LineNum:   beforeLineNums[i],
					Text:      bLine,
					IsContext: true,
				})
			}
			beforeBuf = beforeBuf[:0]
			beforeLineNums = beforeLineNums[:0]

			results = append(results, LineMatch{LineNum: lineNum, Text: line})
			afterRemaining = opts.AfterCtx
		} else if afterRemaining > 0 {
			results = append(results, LineMatch{LineNum: lineNum, Text: line, IsContext: true})
			afterRemaining--
			// Don't add to before-buffer when emitting after-context
		} else {
			// Add to before-context buffer
			if opts.BeforeCtx > 0 {
				if len(beforeBuf) >= opts.BeforeCtx {
					beforeBuf = append(beforeBuf[1:], line)
					beforeLineNums = append(beforeLineNums[1:], lineNum)
				} else {
					beforeBuf = append(beforeBuf, line)
					beforeLineNums = append(beforeLineNums, lineNum)
				}
			}
		}
	}
	return results, scanner.Err()
}

func searchMultiline(ctx context.Context, path string, opts SearchOptions) ([]LineMatch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	matches := opts.Pattern.FindAll(data, -1)
	if len(matches) == 0 {
		return nil, nil
	}
	// For multiline, just return a single LineMatch per match
	var results []LineMatch
	for _, m := range matches {
		results = append(results, LineMatch{LineNum: 0, Text: string(m)})
	}
	return results, nil
}

// CountMatches returns the number of matching lines in a file.
func CountMatches(path string, pattern *regexp.Regexp) (int, error) {
	if isBinaryFile(path) {
		return 0, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if pattern.MatchString(scanner.Text()) {
			count++
		}
	}
	return count, scanner.Err()
}
