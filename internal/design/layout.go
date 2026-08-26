package design

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ManifestName is the portable per-artifact manifest, committed with the files.
const ManifestName = "pando-design.json"

// Layout resolves artifact directories inside a project. Every path handed to
// the rest of the package goes through it, so nothing can address a directory
// outside the configured output root.
type Layout struct {
	// WorkingDir is the absolute project root.
	WorkingDir string
	// OutputDir is the project-relative root holding artifacts ("designer").
	OutputDir string
	// SystemDir is the design-system directory inside OutputDir ("_system").
	SystemDir string
}

// NewLayout builds a Layout, filling empty fields with the defaults.
func NewLayout(workingDir, outputDir, systemDir string) Layout {
	if outputDir == "" {
		outputDir = "designer"
	}
	if systemDir == "" {
		systemDir = "_system"
	}
	return Layout{
		WorkingDir: filepath.Clean(workingDir),
		OutputDir:  filepath.ToSlash(filepath.Clean(outputDir)),
		SystemDir:  systemDir,
	}
}

// Root returns the absolute path of the artifact output root.
func (l Layout) Root() string {
	return filepath.Join(l.WorkingDir, filepath.FromSlash(l.OutputDir))
}

// SystemPath returns the absolute path of the design-system directory.
func (l Layout) SystemPath() string {
	return filepath.Join(l.Root(), l.SystemDir)
}

// RelDir returns the project-relative directory of an artifact slug.
func (l Layout) RelDir(slug string) string {
	return l.OutputDir + "/" + slug
}

// AbsDir returns the absolute directory for a project-relative artifact dir,
// rejecting anything that escapes the output root.
func (l Layout) AbsDir(relDir string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(relDir))
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("design: artifact dir must be project-relative, got %q", relDir)
	}
	abs := filepath.Join(l.WorkingDir, cleaned)
	if err := l.contains(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// contains verifies that abs sits under the output root.
func (l Layout) contains(abs string) error {
	root := l.Root()
	rel, err := filepath.Rel(root, filepath.Clean(abs))
	if err != nil {
		return fmt.Errorf("design: resolve %s against %s: %w", abs, root, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("design: path %s escapes the design output directory %s", abs, root)
	}
	return nil
}

// EnsureRoot creates the output root if it does not exist yet.
func (l Layout) EnsureRoot() error {
	if err := os.MkdirAll(l.Root(), 0o755); err != nil {
		return fmt.Errorf("design: create output dir %s: %w", l.Root(), err)
	}
	return nil
}

// AvailableSlug turns title into a directory-safe slug and appends a numeric
// suffix until it names a directory that does not exist yet.
func (l Layout) AvailableSlug(title string) (string, error) {
	base := Slugify(title)
	if base == "" {
		base = "artifact"
	}
	candidate := base
	for i := 2; i < 1000; i++ {
		abs := filepath.Join(l.Root(), candidate)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("design: stat %s: %w", abs, err)
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", fmt.Errorf("design: no free slug for %q", title)
}

// reservedSlugs are names the output root uses for its own purposes.
var reservedSlugs = map[string]bool{"_system": true}

// Slugify reduces a free-form title to a lowercase, hyphen-separated,
// filesystem-safe name. It never returns a path separator, a leading dot or a
// reserved name.
func Slugify(title string) string {
	var b strings.Builder
	lastHyphen := true // suppresses a leading hyphen
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r) && r < unicode.MaxASCII, unicode.IsDigit(r) && r < unicode.MaxASCII:
			b.WriteRune(r)
			lastHyphen = false
		case r == '_':
			// Underscore is filesystem-safe and meaningful: it is what makes
			// the reserved "_system" directory recognisable.
			b.WriteRune(r)
			lastHyphen = false
		case r == '-' || unicode.IsSpace(r) || r == '/' || r == '.':
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		default:
			// Non-ASCII letters and punctuation collapse into a separator so the
			// slug stays portable across filesystems.
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 64 {
		slug = strings.Trim(slug[:64], "-")
	}
	if reservedSlugs[slug] {
		slug += "-artifact"
	}
	return slug
}

// NewArtifactID returns a fresh artifact identifier.
func NewArtifactID() string {
	return "dsg_" + randomHex(8)
}

// NewCritiqueID returns a fresh critique identifier.
func NewCritiqueID() string {
	return "crt_" + randomHex(8)
}

// randomHex returns n random bytes hex-encoded. It falls back to a
// deterministic-length zero string only if the system entropy source fails,
// which callers surface as a duplicate-id error rather than a silent collision.
func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(buf)
}
