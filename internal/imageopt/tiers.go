package imageopt

// Vision resolution tiers.
//
// Cost of an image for a vision model is driven by pixels, not bytes: the
// provider rescales anything larger than its long-side limit before encoding.
// Resizing locally to that limit saves upload bandwidth and latency with no
// loss of detail the model could have used.
const (
	// DefaultLongSidePx is the long-side pixel cap for standard vision models.
	DefaultLongSidePx = 1568

	// HighTierLongSidePx is the long-side pixel cap for the newest, highest
	// resolution vision models (Opus 4.8, Sonnet 5, Fable 5, Mythos 5).
	HighTierLongSidePx = 2576
)
