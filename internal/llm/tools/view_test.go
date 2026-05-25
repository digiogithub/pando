package tools

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"
)

type stubFileInfo struct {
	size int64
}

func (s stubFileInfo) Name() string       { return "large.txt" }
func (s stubFileInfo) Size() int64        { return s.size }
func (s stubFileInfo) Mode() fs.FileMode  { return 0 }
func (s stubFileInfo) ModTime() time.Time { return time.Time{} }
func (s stubFileInfo) IsDir() bool        { return false }
func (s stubFileInfo) Sys() any           { return nil }

type stubFSReader struct {
	content     []byte
	rangeCalls  int
	readAllUsed bool
}

func (s *stubFSReader) ReadFile(_ context.Context, _ string) ([]byte, error) {
	s.readAllUsed = true
	return s.content, nil
}

func (s *stubFSReader) ReadFileRange(_ context.Context, _ string, offset, length int64) ([]byte, error) {
	s.rangeCalls++
	if offset >= int64(len(s.content)) {
		return []byte{}, nil
	}
	end := offset + length
	if end > int64(len(s.content)) {
		end = int64(len(s.content))
	}
	return s.content[offset:end], nil
}

func TestViewReadFileContentStreamsLargeFiles(t *testing.T) {
	ctx := context.Background()
	content := strings.Repeat("line\n", (MaxReadSize/5)+100)
	fsReader := &stubFSReader{content: []byte(content)}
	tool := &viewTool{}

	result, totalLines, err := tool.readFileContent(ctx, fsReader, "/tmp/large.txt", stubFileInfo{size: int64(len(content))}, 10, 5)
	if err != nil {
		t.Fatalf("readFileContent returned error: %v", err)
	}
	if fsReader.readAllUsed {
		t.Fatal("expected large file path to avoid ReadFile")
	}
	if fsReader.rangeCalls == 0 {
		t.Fatal("expected large file path to use ReadFileRange")
	}
	if totalLines <= 15 {
		t.Fatalf("expected total line count beyond requested window, got %d", totalLines)
	}

	gotLines := strings.Split(result, "\n")
	if len(gotLines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(gotLines))
	}
	for _, line := range gotLines {
		if line != "line" {
			t.Fatalf("unexpected line content: %q", line)
		}
	}
}

func TestReadTextFileStreamingTruncatesLongLines(t *testing.T) {
	ctx := context.Background()
	longLine := strings.Repeat("a", MaxLineLength+50)
	content := []byte(longLine + "\nshort\n")
	fsReader := &stubFSReader{content: content}

	result, totalLines, err := readTextFileStreaming(ctx, fsReader, "/tmp/file.txt", 0, 2)
	if err != nil {
		t.Fatalf("readTextFileStreaming returned error: %v", err)
	}
	if totalLines != 2 {
		t.Fatalf("expected 2 total lines, got %d", totalLines)
	}

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if len(lines[0]) != MaxLineLength+3 {
		t.Fatalf("expected truncated long line length %d, got %d", MaxLineLength+3, len(lines[0]))
	}
	if !strings.HasSuffix(lines[0], "...") {
		t.Fatalf("expected truncated line to end with ellipsis, got %q", lines[0][len(lines[0])-10:])
	}
}
