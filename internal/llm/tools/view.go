package tools

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/logging"
	"github.com/digiogithub/pando/internal/mesnada/acp"
)

type ViewParams struct {
	FilePath string `json:"file_path"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
	EndLine  int    `json:"end_line"`
	// Mode selects the read fidelity (lean-ctx Phase 1). One of "full" (default —
	// raw line-numbered window), "signatures" (every symbol's signature),
	// "map" (imports + top-level signatures) or "auto" (picked by file size/type).
	// Absent or "full" reproduces the legacy output exactly. Compressed modes
	// preserve line numbers so you can follow up with offset/limit to read bodies.
	Mode string `json:"mode,omitempty"`
}

type viewTool struct {
	lspProvider LSPProvider
}

type ViewResponseMetadata struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

const (
	ViewToolName       = "view"
	MaxReadSize        = 250 * 1024
	LargeFileProbeSize = 4096
	DefaultReadLimit   = 2000
	MaxLineLength      = 2000
	viewDescription    = `File viewing tool that reads and displays the contents of files with line numbers, allowing you to examine code, logs, or text data.

WHEN TO USE THIS TOOL:
- Use when you need to read the contents of a specific file
- Helpful for examining source code, configuration files, or log files
- Perfect for looking at text-based file formats

HOW TO USE:
- Provide the path to the file you want to view
- Optionally specify an offset to start reading from a specific line
- Optionally specify a limit to control how many lines are read

FEATURES:
- Displays file contents with line numbers for easy reference
- Can read from any position in a file using the offset parameter
- Handles large files by limiting the number of lines read
- Automatically truncates very long lines for better display
- Suggests similar file names when the requested file isn't found

LIMITATIONS:
- Default reading limit is 2000 lines
- Lines longer than 2000 characters are truncated
- Cannot display binary files or images
- Images can be identified but not displayed

LARGE FILES:
- Files with more than 300 lines trigger automatic session caching
- First 200 lines are shown inline with a cache_id reference in the header
- Use cache_read tool to access subsequent pages of the cached content
- Alternatively, use the offset parameter directly to jump to specific line ranges

TIPS:
- Use with Glob tool to first find files you want to view
- For code exploration, first use Grep to find relevant files, then View to examine them
- When viewing large files, use the offset parameter to read specific sections`
)

func NewViewTool(lspProvider LSPProvider) BaseTool {
	return &viewTool{
		lspProvider,
	}
}

func (v *viewTool) Info() ToolInfo {
	return ToolInfo{
		Name:        ViewToolName,
		Description: viewDescription,
		Parameters: map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The path to the file to read",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "The line number to start reading from (0-based)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "The number of lines to read (defaults to 2000)",
			},
			"end_line": map[string]any{
				"type":        "integer",
				"description": "The line number to stop reading at (exclusive, 0-based). If set, it overrides limit.",
			},
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"full", "signatures", "map", "auto"},
				"description": "Read fidelity. 'full' (default) returns the raw line-numbered window. 'signatures' returns every symbol's signature, 'map' returns imports + top-level signatures, 'auto' picks by file size/type. Compressed modes keep line numbers so you can re-read a body with offset/limit. Use 'full' when you need exact source text.",
			},
		},
		Required: []string{"file_path"},
	}
}

// Run implements Tool.
func (v *viewTool) Run(ctx context.Context, call ToolCall) (ToolResponse, error) {
	var params ViewParams
	logging.Debug("view tool params", "params", call.Input)
	if err := DecodeToolInput(call.Input, &params); err != nil {
		return NewTextErrorResponse(fmt.Sprintf("error parsing parameters: %s", err)), nil
	}

	if params.FilePath == "" {
		return NewTextErrorResponse("file_path is required"), nil
	}

	logging.Debug("view tool called", "filePath", params.FilePath, "offset", params.Offset, "limit", params.Limit)

	// Check if we're in ACP context and should use client callbacks
	if acpConn := ctx.Value(ACPClientConnContextKey); acpConn != nil {
		return v.runWithACP(ctx, params, acpConn)
	}

	// Resolve path (handles relative paths and prevents workdir doubling)
	filePath := resolveToolPath(params.FilePath)
	workspaceFS := getWorkspaceFS(ctx)

	// Check if file exists
	fileInfo, err := workspaceFS.Stat(ctx, filePath)
	if err != nil {
		if isNotExist(err) {
			// Try to offer suggestions for similarly named files
			dir := filepath.Dir(filePath)
			base := filepath.Base(filePath)

			dirEntries, dirErr := workspaceFS.List(ctx, dir)
			if dirErr == nil {
				var suggestions []string
				for _, entry := range dirEntries {
					if strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(base)) ||
						strings.Contains(strings.ToLower(base), strings.ToLower(entry.Name())) {
						suggestions = append(suggestions, filepath.Join(dir, entry.Name()))
						if len(suggestions) >= 3 {
							break
						}
					}
				}

				if len(suggestions) > 0 {
					return NewTextErrorResponse(fmt.Sprintf("File not found: %s\n\nDid you mean one of these?\n%s",
						filePath, strings.Join(suggestions, "\n"))), nil
				}
			}

			return NewTextErrorResponse(fmt.Sprintf("File not found: %s", filePath)), nil
		}
		return ToolResponse{}, fmt.Errorf("error accessing file: %w", err)
	}

	// Check if it's a directory
	if fileInfo.IsDir() {
		return NewTextErrorResponse(fmt.Sprintf("Path is a directory, not a file: %s", filePath)), nil
	}

	// Set default limit if not provided
	if params.EndLine > params.Offset {
		params.Limit = params.EndLine - params.Offset
	} else if params.Limit <= 0 {
		params.Limit = DefaultReadLimit
	}

	// Check if it's an image file
	isImage, imageType := isImageFile(filePath)
	// TODO: handle images
	if isImage {
		return NewTextErrorResponse(fmt.Sprintf("This is an image file of type: %s\nUse a different tool to process images", imageType)), nil
	}

	content, lineCount, err := v.readFileContent(ctx, workspaceFS, filePath, fileInfo, params.Offset, params.Limit)
	if err != nil {
		if errors.Is(err, errBinaryContent) {
			return NewTextErrorResponse(fmt.Sprintf("File appears to be binary: %s", filePath)), nil
		}
		return ToolResponse{}, fmt.Errorf("error reading file: %w", err)
	}

	logging.Debug("view file read", "filePath", filePath, "lineCount", lineCount)

	windowEnd := params.Offset + len(strings.Split(content, "\n"))

	// Phase 2: collapse an unchanged (or diff a changed) re-read of the same window.
	if resp, handled := dedupViewRead(ctx, filePath, content, params.Offset, windowEnd); handled {
		recordFileRead(filePath)
		return resp, nil
	}

	v.lspProvider.EnsureForFile(ctx, filePath)
	clients := v.lspProvider.ClientsForFile(filePath)
	notifyLspOpenFile(ctx, filePath, clients)
	diagnostics := getDiagnostics(filePath, clients)

	// Phase 1: optional compressed read modes (signatures / map / auto). Falls
	// back to the raw full window on parse error or when not actually smaller.
	if body, mode, ok := renderViewMode(ctx, filePath, content, params.Offset, int(fileInfo.Size()), params.Mode, strings.TrimSpace(diagnostics) != ""); ok {
		output := fmt.Sprintf("<file path=%s mode=%s lines=%d-%d>\n", displayPath(filePath), mode, params.Offset+1, windowEnd)
		output += body
		output += "\n</file>\n"
		output += diagnostics
		recordFileRead(filePath)
		return WithResponseMetadata(
			NewTextResponse(output),
			ViewResponseMetadata{
				FilePath: filePath,
				Content:  content,
			},
		), nil
	}

	output := "<file>\n"
	// Format the output with line numbers
	output += addLineNumbers(content, params.Offset+1)

	// Add a note if the content was truncated
	if lineCount > params.Offset+len(strings.Split(content, "\n")) {
		output += fmt.Sprintf("\n\n(File has more lines. Use 'offset' parameter to read beyond line %d)",
			params.Offset+len(strings.Split(content, "\n")))
	}
	output += "\n</file>\n"
	output += diagnostics
	recordFileRead(filePath)
	return WithResponseMetadata(
		NewTextResponse(output),
		ViewResponseMetadata{
			FilePath: filePath,
			Content:  content,
		},
	), nil
}

var errBinaryContent = errors.New("binary content")

func (v *viewTool) readFileContent(ctx context.Context, workspaceFS fsReader, filePath string, fileInfo fs.FileInfo, offset, limit int) (string, int, error) {
	if fileInfo.Size() <= MaxReadSize {
		fileContent, err := workspaceFS.ReadFile(ctx, filePath)
		if err != nil {
			return "", 0, err
		}

		sampleSize := min(len(fileContent), binarySampleSize)
		if sampleSize > 0 && isBinaryContent(fileContent[:sampleSize]) {
			return "", 0, errBinaryContent
		}

		return readTextFile(fileContent, offset, limit)
	}

	probe, err := workspaceFS.ReadFileRange(ctx, filePath, 0, LargeFileProbeSize)
	if err != nil {
		return "", 0, err
	}
	if len(probe) > 0 && isBinaryContent(probe) {
		return "", 0, errBinaryContent
	}

	return readTextFileStreaming(ctx, workspaceFS, filePath, offset, limit)
}

type fsReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, error)
	ReadFileRange(ctx context.Context, path string, offset, length int64) ([]byte, error)
}

func addLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}

	lines := strings.Split(content, "\n")

	var result []string
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")

		lineNum := i + startLine
		numStr := fmt.Sprintf("%d", lineNum)

		if len(numStr) >= 6 {
			result = append(result, fmt.Sprintf("%s|%s", numStr, line))
		} else {
			paddedNum := fmt.Sprintf("%6s", numStr)
			result = append(result, fmt.Sprintf("%s|%s", paddedNum, line))
		}
	}

	return strings.Join(result, "\n")
}

func readTextFile(content []byte, offset, limit int) (string, int, error) {
	lineCount := 0

	reader := bytes.NewReader(content)
	scanner := NewLineScanner(reader)
	if offset > 0 {
		for lineCount < offset && scanner.Scan() {
			lineCount++
		}
		if err := scanner.Err(); err != nil {
			return "", 0, err
		}
	}

	if offset == 0 {
		_, err := reader.Seek(0, io.SeekStart)
		if err != nil {
			return "", 0, err
		}
		scanner = NewLineScanner(reader)
	}

	var lines []string
	lineCount = offset

	for scanner.Scan() && len(lines) < limit {
		lineCount++
		lineText := scanner.Text()
		if len(lineText) > MaxLineLength {
			lineText = lineText[:MaxLineLength] + "..."
		}
		lines = append(lines, lineText)
	}

	// Continue scanning to get total line count
	for scanner.Scan() {
		lineCount++
	}

	if err := scanner.Err(); err != nil {
		return "", 0, err
	}

	return strings.Join(lines, "\n"), lineCount, nil
}

func readTextFileStreaming(ctx context.Context, workspaceFS fsReader, filePath string, offset, limit int) (string, int, error) {
	const chunkSize int64 = 64 * 1024

	var (
		chunkOffset int64
		leftover    string
		lines       []string
		lineCount   int
	)

	for {
		chunk, err := workspaceFS.ReadFileRange(ctx, filePath, chunkOffset, chunkSize)
		if err != nil {
			return "", 0, err
		}
		if len(chunk) == 0 {
			break
		}

		chunkOffset += int64(len(chunk))
		text := leftover + string(chunk)
		parts := strings.Split(text, "\n")
		leftover = parts[len(parts)-1]

		for _, part := range parts[:len(parts)-1] {
			if lineCount >= offset && len(lines) < limit {
				lines = append(lines, truncateLongLine(strings.TrimSuffix(part, "\r")))
			}
			lineCount++
		}

		if len(chunk) < int(chunkSize) {
			break
		}
	}

	if leftover != "" || chunkOffset == 0 {
		if lineCount >= offset && len(lines) < limit {
			lines = append(lines, truncateLongLine(strings.TrimSuffix(leftover, "\r")))
		}
		lineCount++
	}

	if offset > 0 && lineCount <= offset {
		return "", lineCount, nil
	}

	return strings.Join(lines, "\n"), lineCount, nil
}

func truncateLongLine(line string) string {
	if len(line) > MaxLineLength {
		return line[:MaxLineLength] + "..."
	}
	return line
}

const binarySampleSize = 4096

// isBinaryContent detects binary files using null-byte and non-printable ratio checks.
func isBinaryContent(data []byte) bool {
	// Null byte is a strong indicator of binary content
	if bytes.IndexByte(data, 0) != -1 {
		return true
	}
	// Count non-printable, non-whitespace bytes
	nonPrintable := 0
	for _, b := range data {
		if b < 0x09 || (b > 0x0D && b < 0x20) || b > 0x7E {
			nonPrintable++
		}
	}
	return len(data) > 0 && float64(nonPrintable)/float64(len(data)) > 0.30
}

func isImageFile(filePath string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg":
		return true, "JPEG"
	case ".png":
		return true, "PNG"
	case ".gif":
		return true, "GIF"
	case ".bmp":
		return true, "BMP"
	case ".svg":
		return true, "SVG"
	case ".webp":
		return true, "WebP"
	default:
		return false, ""
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type LineScanner struct {
	scanner *bufio.Scanner
}

func NewLineScanner(r io.Reader) *LineScanner {
	return &LineScanner{
		scanner: bufio.NewScanner(r),
	}
}

func (s *LineScanner) Scan() bool {
	return s.scanner.Scan()
}

func (s *LineScanner) Text() string {
	return s.scanner.Text()
}

func (s *LineScanner) Err() error {
	return s.scanner.Err()
}

// runWithACP handles file reading via ACP client callback.
func (v *viewTool) runWithACP(ctx context.Context, params ViewParams, acpConnInterface interface{}) (ToolResponse, error) {
	acpConn, ok := acpConnInterface.(*acp.ACPClientConnection)
	if !ok {
		return ToolResponse{}, fmt.Errorf("invalid ACP client connection type")
	}

	logging.Debug("view tool using ACP callback", "filePath", params.FilePath)

	// Read file content via client callback
	content, err := acpConn.ReadTextFile(ctx, params.FilePath)
	if err != nil {
		// Return user-friendly error
		return NewTextErrorResponse(fmt.Sprintf("Failed to read file: %s", err)), nil
	}

	// Apply params logic for end_line
	if params.EndLine > params.Offset {
		params.Limit = params.EndLine - params.Offset
	} else if params.Limit <= 0 {
		params.Limit = DefaultReadLimit
	}

	// Process the content (apply offset and limit)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// Apply offset
	startLine := params.Offset
	if startLine >= totalLines {
		return NewTextErrorResponse(fmt.Sprintf("Offset %d exceeds file length (%d lines)", startLine, totalLines)), nil
	}

	// Apply limit
	endLine := startLine + params.Limit
	if endLine > totalLines {
		endLine = totalLines
	}

	// Extract the requested lines
	selectedLines := lines[startLine:endLine]

	// Truncate long lines
	for i, line := range selectedLines {
		if len(line) > MaxLineLength {
			selectedLines[i] = line[:MaxLineLength] + "..."
		}
	}

	processedContent := strings.Join(selectedLines, "\n")

	// Phase 2: collapse an unchanged (or diff a changed) re-read of the same window.
	if resp, handled := dedupViewRead(ctx, params.FilePath, processedContent, params.Offset, endLine); handled {
		recordFileRead(params.FilePath)
		return resp, nil
	}

	// Phase 1: optional compressed read modes. ACP reads have no diagnostics.
	if body, mode, ok := renderViewMode(ctx, params.FilePath, processedContent, params.Offset, len(content), params.Mode, false); ok {
		out := fmt.Sprintf("<file path=%s mode=%s lines=%d-%d>\n", displayPath(params.FilePath), mode, params.Offset+1, endLine)
		out += body
		out += "\n</file>\n"
		recordFileRead(params.FilePath)
		return WithResponseMetadata(
			NewTextResponse(out),
			ViewResponseMetadata{FilePath: params.FilePath, Content: processedContent},
		), nil
	}

	// Format output with line numbers
	output := "<file>\n"
	output += addLineNumbers(processedContent, params.Offset+1)

	// Add note if content was truncated
	if endLine < totalLines {
		output += fmt.Sprintf("\n\n(File has more lines. Use 'offset' parameter to read beyond line %d)", endLine)
	}
	output += "\n</file>\n"

	logging.Debug("view ACP completed", "filePath", params.FilePath, "linesRead", len(selectedLines))

	return WithResponseMetadata(
		NewTextResponse(output),
		ViewResponseMetadata{
			FilePath: params.FilePath,
			Content:  processedContent,
		},
	), nil
}
