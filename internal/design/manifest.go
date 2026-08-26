package design

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Viewport is the render size a preview and screenshots default to.
type Viewport struct {
	W int `json:"w"`
	H int `json:"h"`
}

// PreviewSpec holds render defaults carried with the artifact.
type PreviewSpec struct {
	Viewport Viewport `json:"viewport"`
}

// DeckSpec carries deck-only metadata. Slides is informational: the renderer
// re-counts slides on every render, since the files are edited by hand.
type DeckSpec struct {
	Slides     int    `json:"slides,omitempty"`
	Navigation string `json:"navigation,omitempty"` // "horizontal" | "vertical"
}

// Manifest is pando-design.json: the portable, committable description of an
// artifact. It is the source of truth when the SQLite metadata is missing (a
// fresh clone, another machine), so a design directory alone is enough to
// re-adopt the artifact.
type Manifest struct {
	ID           string      `json:"id"`
	Kind         Kind        `json:"kind"`
	Title        string      `json:"title,omitempty"`
	Version      int         `json:"version"`
	Entry        string      `json:"entry"`
	DesignSystem string      `json:"designSystem,omitempty"`
	Skill        string      `json:"skill,omitempty"`
	Preview      PreviewSpec `json:"preview"`
	Deck         *DeckSpec   `json:"deck,omitempty"`
}

// DefaultViewport is the render size used when a manifest does not set one.
var DefaultViewport = Viewport{W: 1440, H: 900}

// NewManifest builds a manifest with the defaults for a kind.
func NewManifest(id string, kind Kind, title string) Manifest {
	m := Manifest{
		ID:      id,
		Kind:    kind,
		Title:   title,
		Version: 1,
		Entry:   "index.html",
		Preview: PreviewSpec{Viewport: DefaultViewport},
	}
	if kind == KindDeck {
		m.Deck = &DeckSpec{Navigation: "horizontal"}
	}
	return m
}

// Normalize fills in missing fields so a hand-edited manifest stays usable.
func (m *Manifest) Normalize() {
	if m.Entry == "" {
		m.Entry = "index.html"
	}
	if m.Version < 1 {
		m.Version = 1
	}
	if m.Preview.Viewport.W <= 0 {
		m.Preview.Viewport.W = DefaultViewport.W
	}
	if m.Preview.Viewport.H <= 0 {
		m.Preview.Viewport.H = DefaultViewport.H
	}
	if !ValidKind(m.Kind) {
		m.Kind = KindWeb
	}
	if m.Kind == KindDeck && m.Deck == nil {
		m.Deck = &DeckSpec{Navigation: "horizontal"}
	}
	if m.Kind != KindDeck {
		m.Deck = nil
	}
}

// ReadManifest loads pando-design.json from an artifact directory.
func ReadManifest(absDir string) (Manifest, error) {
	path := filepath.Join(absDir, ManifestName)
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("design: read manifest %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("design: parse manifest %s: %w", path, err)
	}
	m.Normalize()
	return m, nil
}

// WriteManifest writes pando-design.json into an artifact directory. The file
// is committed with the artifact, so it is formatted for humans to read.
func WriteManifest(absDir string, m Manifest) error {
	m.Normalize()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("design: encode manifest: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return fmt.Errorf("design: create artifact dir %s: %w", absDir, err)
	}
	path := filepath.Join(absDir, ManifestName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("design: write manifest %s: %w", path, err)
	}
	return nil
}
