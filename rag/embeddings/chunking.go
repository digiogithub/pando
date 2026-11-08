package embeddings

import (
	"unicode"
)

// DefaultChunkSize is the default maximum size for text chunks in characters.
const DefaultChunkSize = 800

// DefaultChunkOverlap is the default overlap between consecutive chunks.
const DefaultChunkOverlap = 100

// ChunkText splits text into chunks with sentence-aware splitting.
// It tries to split on sentence boundaries to preserve semantic coherence.
//
// Parameters:
//   - text: The text to split into chunks
//   - maxSize: Maximum size of each chunk in characters
//   - overlap: Number of characters to overlap between consecutive chunks
//
// Returns a slice of text chunks.
func ChunkText(text string, maxSize, overlap int) []string {
	if maxSize <= 0 {
		maxSize = DefaultChunkSize
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxSize {
		overlap = maxSize / 2
	}

	if len(text) <= maxSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	step := maxSize - overlap
	if step <= 0 {
		step = 1
	}
	maxChunks := (len(text) / step) + 2

	for start < len(text) {
		if len(chunks) >= maxChunks {
			chunks = append(chunks, text[start:])
			break
		}

		prevStart := start
		end := start + maxSize
		if end >= len(text) {
			// Last chunk
			chunks = append(chunks, text[start:])
			break
		}

		// Try to find a sentence boundary within the chunk
		chunk := text[start:end]
		splitIdx := findSentenceBoundary(chunk)

		if splitIdx > overlap {
			// Found a good sentence boundary
			chunks = append(chunks, text[start:start+splitIdx])
			start = start + splitIdx - overlap
		} else {
			// No usable sentence boundary found, split at maxSize
			chunks = append(chunks, chunk)
			start = end - overlap
		}

		// Ensure we don't go backwards
		if start < 0 {
			start = 0
		}

		// Safety: enforce forward progress to avoid infinite loops when
		// split index is too small compared to overlap.
		if start <= prevStart {
			fallback := prevStart + (maxSize - overlap)
			if fallback <= prevStart {
				fallback = prevStart + 1
			}
			start = fallback
		}
	}

	return chunks
}

// findSentenceBoundary finds the last sentence boundary in the text.
// Returns the index after the sentence-ending punctuation, or 0 if none found.
func findSentenceBoundary(text string) int {
	// Look for common sentence endings: . ! ? followed by space or end
	lastIdx := 0
	inQuote := false

	for i, r := range text {
		if r == '"' || r == '\'' {
			inQuote = !inQuote
		}

		// Check for sentence-ending punctuation
		if !inQuote && (r == '.' || r == '!' || r == '?') {
			// Make sure it's followed by space, newline, or end of text
			if i+1 < len(text) {
				next := rune(text[i+1])
				if unicode.IsSpace(next) || next == '\n' {
					lastIdx = i + 1
					// Skip trailing whitespace
					for lastIdx < len(text) && unicode.IsSpace(rune(text[lastIdx])) {
						lastIdx++
					}
				}
			} else {
				lastIdx = i + 1
			}
		}
	}

	return lastIdx
}

// AverageEmbeddings computes the average of multiple embeddings.
// This is useful for combining embeddings of multiple chunks into a single document embedding.
//
// Returns nil if embeddings is empty or if embeddings have inconsistent dimensions.
func AverageEmbeddings(embeddings [][]float32) []float32 {
	if len(embeddings) == 0 {
		return nil
	}

	dim := len(embeddings[0])
	if dim == 0 {
		return nil
	}

	// Check dimension consistency
	for _, emb := range embeddings {
		if len(emb) != dim {
			return nil
		}
	}

	// Compute average
	avg := make([]float32, dim)
	count := float32(len(embeddings))

	for _, emb := range embeddings {
		for i, v := range emb {
			avg[i] += v / count
		}
	}

	return avg
}

// SplitAndEmbed is a helper that chunks text and generates embeddings for all chunks.
// It's a convenience function that combines ChunkText and Embedder.EmbedDocuments.
//
// Parameters:
//   - embedder: The embedder to use
//   - text: The text to chunk and embed
//   - maxSize: Maximum chunk size
//   - overlap: Chunk overlap
//
// Returns embeddings for all chunks.
func SplitAndEmbed(embedder Embedder, text string, maxSize, overlap int) ([][]float32, error) {
	chunks := ChunkText(text, maxSize, overlap)
	return embedder.EmbedDocuments(nil, chunks)
}

// SplitAndEmbedAverage chunks text, generates embeddings, and averages them.
// This produces a single embedding representing the entire text.
//
// Returns a single averaged embedding.
func SplitAndEmbedAverage(embedder Embedder, text string, maxSize, overlap int) ([]float32, error) {
	embeddings, err := SplitAndEmbed(embedder, text, maxSize, overlap)
	if err != nil {
		return nil, err
	}
	return AverageEmbeddings(embeddings), nil
}
