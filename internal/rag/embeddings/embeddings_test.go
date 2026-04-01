package embeddings

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestNewEmbedder verifies that the factory creates embedders correctly.
func TestNewEmbedder(t *testing.T) {
	tests := []struct {
		name        string
		provider    string
		model       string
		apiKey      string
		baseURL     string
		expectError bool
	}{
		{
			name:        "OpenAI with API key",
			provider:    "openai",
			model:       "text-embedding-3-small",
			apiKey:      "test-key",
			baseURL:     "",
			expectError: false,
		},
		{
			name:        "OpenAI without API key",
			provider:    "openai",
			model:       "text-embedding-3-small",
			apiKey:      "",
			baseURL:     "",
			expectError: true,
		},
		{
			name:        "Google with API key",
			provider:    "google",
			model:       "text-embedding-004",
			apiKey:      "test-key",
			baseURL:     "",
			expectError: false,
		},
		{
			name:        "Ollama local",
			provider:    "ollama",
			model:       "nomic-embed-text",
			apiKey:      "",
			baseURL:     "",
			expectError: false,
		},
		{
			name:        "Anthropic/Voyage with API key",
			provider:    "anthropic",
			model:       "voyage-3",
			apiKey:      "test-key",
			baseURL:     "",
			expectError: false,
		},
		{
			name:        "Unsupported provider",
			provider:    "unsupported",
			model:       "some-model",
			apiKey:      "test-key",
			baseURL:     "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embedder, err := NewEmbedder(tt.provider, tt.model, tt.apiKey, tt.baseURL)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if embedder == nil {
					t.Errorf("Expected embedder but got nil")
				}
			}
		})
	}
}

// TestGetDefaultModel verifies default model retrieval.
func TestGetDefaultModel(t *testing.T) {
	tests := []struct {
		provider string
		expected string
	}{
		{"openai", "text-embedding-3-small"},
		{"google", "text-embedding-004"},
		{"gemini", "text-embedding-004"},
		{"ollama", "nomic-embed-text"},
		{"anthropic", "voyage-3"},
		{"voyage", "voyage-3"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			result := GetDefaultModel(tt.provider)
			if result != tt.expected {
				t.Errorf("Expected %q but got %q", tt.expected, result)
			}
		})
	}
}

// TestGetModelDimension verifies dimension lookup.
func TestGetModelDimension(t *testing.T) {
	tests := []struct {
		model    string
		expected int
	}{
		{"text-embedding-3-small", 1536},
		{"text-embedding-3-large", 3072},
		{"text-embedding-ada-002", 1536},
		{"text-embedding-004", 768},
		{"nomic-embed-text", 768},
		{"voyage-3", 1024},
		{"unknown-model", 0},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			result := GetModelDimension(tt.model)
			if result != tt.expected {
				t.Errorf("Expected %d but got %d", tt.expected, result)
			}
		})
	}
}

// TestChunkText verifies text chunking.
func TestChunkText(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		maxSize   int
		overlap   int
		minChunks int
		maxChunks int
	}{
		{
			name:      "Short text",
			text:      "This is a short text.",
			maxSize:   100,
			overlap:   10,
			minChunks: 1,
			maxChunks: 1,
		},
		{
			name:      "Long text with sentences",
			text:      "This is sentence one. This is sentence two. This is sentence three. This is sentence four. This is sentence five.",
			maxSize:   50,
			overlap:   10,
			minChunks: 2,
			maxChunks: 5,
		},
		{
			name:      "Empty text",
			text:      "",
			maxSize:   100,
			overlap:   10,
			minChunks: 1,
			maxChunks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkText(tt.text, tt.maxSize, tt.overlap)
			if len(chunks) < tt.minChunks || len(chunks) > tt.maxChunks {
				t.Errorf("Expected %d-%d chunks but got %d", tt.minChunks, tt.maxChunks, len(chunks))
			}

			// Verify chunk sizes
			for i, chunk := range chunks {
				if len(chunk) > tt.maxSize && i < len(chunks)-1 {
					t.Errorf("Chunk %d exceeds maxSize: %d > %d", i, len(chunk), tt.maxSize)
				}
			}
		})
	}
}

func TestChunkText_ProgressWhenBoundarySmallerThanOverlap(t *testing.T) {
	text := "A. \"" + strings.Repeat("x", 500)

	done := make(chan []string, 1)
	go func() {
		done <- ChunkText(text, 80, 50)
	}()

	select {
	case chunks := <-done:
		if len(chunks) == 0 {
			t.Fatalf("expected non-empty chunks")
		}
		if len(chunks) > 100 {
			t.Fatalf("unexpectedly high chunk count (possible low progress): %d", len(chunks))
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("ChunkText appears stuck (no forward progress)")
	}
}

// TestAverageEmbeddings verifies embedding averaging.
func TestAverageEmbeddings(t *testing.T) {
	tests := []struct {
		name       string
		embeddings [][]float32
		expected   []float32
	}{
		{
			name: "Two embeddings",
			embeddings: [][]float32{
				{1.0, 2.0, 3.0},
				{3.0, 4.0, 5.0},
			},
			expected: []float32{2.0, 3.0, 4.0},
		},
		{
			name:       "Empty embeddings",
			embeddings: [][]float32{},
			expected:   nil,
		},
		{
			name: "Inconsistent dimensions",
			embeddings: [][]float32{
				{1.0, 2.0},
				{3.0, 4.0, 5.0},
			},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AverageEmbeddings(tt.embeddings)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil but got %v", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("Expected length %d but got %d", len(tt.expected), len(result))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("At index %d: expected %f but got %f", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

// TestOllamaEmbedderDimension verifies dimension tracking.
func TestOllamaEmbedderDimension(t *testing.T) {
	embedder, err := NewOllamaEmbedder("nomic-embed-text", "http://localhost:11434")
	if err != nil {
		t.Fatalf("Failed to create embedder: %v", err)
	}

	// Initially dimension should be 0
	if embedder.Dimension() != 0 {
		t.Errorf("Expected initial dimension 0 but got %d", embedder.Dimension())
	}
}

// BenchmarkChunkText benchmarks text chunking.
func BenchmarkChunkText(b *testing.B) {
	text := "This is a test sentence. " +
		"It contains multiple sentences for testing. " +
		"We want to see how fast chunking performs. " +
		"More text means more realistic benchmarks. " +
		"Let's add some more sentences here. " +
		"And a few more for good measure. " +
		"This should be enough text now."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ChunkText(text, 100, 20)
	}
}

// Example demonstrates basic usage of the embeddings package.
func ExampleNewEmbedder() {
	// Create an Ollama embedder (doesn't require API key)
	embedder, err := NewEmbedder("ollama", "nomic-embed-text", "", "")
	if err != nil {
		panic(err)
	}

	// Generate embeddings (would require actual Ollama instance)
	ctx := context.Background()
	texts := []string{"Hello world", "This is a test"}
	embeddings, err := embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		// Handle error (in tests we expect this to fail without Ollama running)
		return
	}

	// Use embeddings
	_ = embeddings
}
