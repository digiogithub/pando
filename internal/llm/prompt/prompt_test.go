package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/skills"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetContextFromPaths(t *testing.T) {
	resetContextCache()
	t.Cleanup(resetContextCache)

	tmpDir := t.TempDir()
	_, err := config.Load(tmpDir, false)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	cfg := config.Get()
	cfg.WorkingDir = tmpDir
	cfg.ContextPaths = []string{
		"file.txt",
		"directory/",
	}
	testFiles := []string{
		"file.txt",
		"directory/file_a.txt",
		"directory/file_b.txt",
		"directory/file_c.txt",
	}

	createTestFiles(t, tmpDir, testFiles)

	context := getContextFromPaths()
	expectedContext := "# From:file.txt\nfile.txt: test content\n# From:directory/file_a.txt\ndirectory/file_a.txt: test content\n# From:directory/file_b.txt\ndirectory/file_b.txt: test content\n# From:directory/file_c.txt\ndirectory/file_c.txt: test content"
	assert.Equal(t, expectedContext, context)
}

func TestProcessContextPathsUsesFirstProjectMemoryFileByPriority(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{"AGENTS.md", "PANDO.md", "CLAUDE.md"})

	context := processContextPaths(tmpDir, []string{"AGENTS.md", "PANDO.md", "CLAUDE.md"})
	expected := "# From:AGENTS.md\nAGENTS.md: test content"
	assert.Equal(t, expected, context)
}

func TestProcessContextPathsFallsBackToNextPriorityFile(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{"PANDO.md", "CLAUDE.md"})

	context := processContextPaths(tmpDir, []string{"AGENTS.md", "PANDO.md", "CLAUDE.md"})
	expected := "# From:PANDO.md\nPANDO.md: test content"
	assert.Equal(t, expected, context)
}

func TestLoadContextFilesUsesFirstProjectMemoryFileByPriority(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{"AGENTS.md", "CLAUDE.md", "docs/guide.md"})

	files := LoadContextFiles(tmpDir, []string{"AGENTS.md", "CLAUDE.md", "docs/"})
	require.Len(t, files, 2)
	assert.Equal(t, "AGENTS.md", files[0].Path)
	assert.Equal(t, "AGENTS.md: test content", files[0].Content)
	assert.Equal(t, "docs/guide.md", files[1].Path)
	assert.Equal(t, "docs/guide.md: test content", files[1].Content)
}

func TestLoadContextFilesAutoIncludesPreferredProjectMemoryWhenPathsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFiles(t, tmpDir, []string{"AGENTS.md", "CLAUDE.md"})

	files := LoadContextFiles(tmpDir, nil)
	require.Len(t, files, 1)
	assert.Equal(t, "AGENTS.md", files[0].Path)
	assert.Equal(t, "AGENTS.md: test content", files[0].Content)
}

func TestInjectSkillsMetadata(t *testing.T) {
	t.Parallel()

	got := InjectSkillsMetadata([]skills.SkillMetadata{
		{
			Name:        "sql",
			Description: "  Tune SQL queries safely. ",
			WhenToUse:   " postgres tuning, slow queries ",
		},
		{
			Name:        "docs",
			Description: "Write concise release notes.",
		},
	})

	want := "## Available Skills\n- **sql**: Tune SQL queries safely. (use when: postgres tuning, slow queries)\n- **docs**: Write concise release notes."
	assert.Equal(t, want, got)
}

func TestInjectSkillInstructions(t *testing.T) {
	t.Parallel()

	got := InjectSkillInstructions(" sql ", "\nUse EXPLAIN ANALYZE before rewriting queries.\n")
	want := "## Active Skill: sql\nUse EXPLAIN ANALYZE before rewriting queries."
	assert.Equal(t, want, got)
}

func createTestFiles(t *testing.T, tmpDir string, testFiles []string) {
	t.Helper()
	for _, path := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if path[len(path)-1] == '/' {
			err := os.MkdirAll(fullPath, 0755)
			require.NoError(t, err)
		} else {
			dir := filepath.Dir(fullPath)
			err := os.MkdirAll(dir, 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(path+": test content"), 0644)
			require.NoError(t, err)
		}
	}
}

func resetContextCache() {
}
