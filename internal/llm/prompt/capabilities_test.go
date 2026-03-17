package prompt

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestCapabilityDetectorNoCapabilities(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil, nil, nil)
	caps := detector.Detect()

	assert.False(t, caps["remembrances"])
	assert.False(t, caps["orchestration"])
	assert.False(t, caps["web_search"])
	assert.False(t, caps["code_indexing"])
	assert.False(t, caps["lsp"])
}

func TestCapabilityDetectorRemembrances(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil, []string{"remembrances"}, nil)
	caps := detector.Detect()

	assert.True(t, caps["remembrances"])
	assert.False(t, caps["orchestration"])
}

func TestCapabilityDetectorMesnada(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil, []string{"mesnada"}, nil)
	caps := detector.Detect()

	assert.True(t, caps["orchestration"])
	assert.False(t, caps["remembrances"])
}

func TestCapabilityDetectorWebSearch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tools []string
	}{
		{"google", []string{"google_search"}},
		{"brave", []string{"brave_search"}},
		{"perplexity", []string{"perplexity_search"}},
		{"web_search", []string{"web_search"}},
		{"fetch", []string{"fetch"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detector := NewCapabilityDetector(nil, nil, tt.tools)
			caps := detector.Detect()
			assert.True(t, caps["web_search"], "expected web_search for tool %v", tt.tools)
		})
	}
}

func TestCapabilityDetectorCodeIndexing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		tools []string
	}{
		{"hybrid_search", []string{"code_hybrid_search"}},
		{"find_symbol", []string{"code_find_symbol"}},
		{"index_project", []string{"code_index_project"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detector := NewCapabilityDetector(nil, nil, tt.tools)
			caps := detector.Detect()
			assert.True(t, caps["code_indexing"], "expected code_indexing for tool %v", tt.tools)
		})
	}
}

func TestCapabilityDetectorLSP(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		LSP: map[string]config.LSPConfig{
			"gopls": {Disabled: false},
		},
	}
	detector := NewCapabilityDetector(cfg, nil, nil)
	caps := detector.Detect()
	assert.True(t, caps["lsp"])
}

func TestCapabilityDetectorLSPDisabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		LSP: map[string]config.LSPConfig{
			"gopls": {Disabled: true},
		},
	}
	detector := NewCapabilityDetector(cfg, nil, nil)
	caps := detector.Detect()
	assert.False(t, caps["lsp"])
}

func TestCapabilityDetectorAllCapabilities(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		LSP: map[string]config.LSPConfig{
			"gopls": {Disabled: false},
		},
	}
	detector := NewCapabilityDetector(cfg,
		[]string{"remembrances", "mesnada"},
		[]string{"google_search", "code_hybrid_search"},
	)
	caps := detector.Detect()

	assert.True(t, caps["remembrances"])
	assert.True(t, caps["orchestration"])
	assert.True(t, caps["web_search"])
	assert.True(t, caps["code_indexing"])
	assert.True(t, caps["lsp"])
}

func TestCapabilityDetectorCaseInsensitive(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil,
		[]string{"Remembrances", "MESNADA"},
		[]string{"Google_Search", "CODE_HYBRID_SEARCH"},
	)
	caps := detector.Detect()

	assert.True(t, caps["remembrances"])
	assert.True(t, caps["orchestration"])
	assert.True(t, caps["web_search"])
	assert.True(t, caps["code_indexing"])
}

func TestDetectWebSearchDetails(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil, nil,
		[]string{"google_search", "brave_search", "perplexity_search"},
	)
	google, brave, perplexity := detector.DetectWebSearchDetails()
	assert.True(t, google)
	assert.True(t, brave)
	assert.True(t, perplexity)
}

func TestDetectWebSearchDetailsPartial(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil, nil, []string{"brave_search"})
	google, brave, perplexity := detector.DetectWebSearchDetails()
	assert.False(t, google)
	assert.True(t, brave)
	assert.False(t, perplexity)
}

func TestDetectWebSearchDetailsNone(t *testing.T) {
	t.Parallel()
	detector := NewCapabilityDetector(nil, nil, nil)
	google, brave, perplexity := detector.DetectWebSearchDetails()
	assert.False(t, google)
	assert.False(t, brave)
	assert.False(t, perplexity)
}
