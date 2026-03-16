package prompt

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	"github.com/digiogithub/pando/internal/logging"
)

//go:embed templates
var embeddedTemplates embed.FS

// TemplateRegistry manages template loading, caching, and rendering.
// It supports embedded templates with external override from project
// or user configuration directories.
type TemplateRegistry struct {
	mu          sync.RWMutex
	cache       map[string]*template.Template
	customFuncs template.FuncMap
	overrideDirs []string
}

// NewTemplateRegistry creates a new TemplateRegistry.
// overrideDirs are directories checked (in order) for template overrides.
// Typically: [".pando/templates", "~/.config/pando/templates"]
func NewTemplateRegistry(overrideDirs ...string) *TemplateRegistry {
	return &TemplateRegistry{
		cache:        make(map[string]*template.Template),
		customFuncs:  make(template.FuncMap),
		overrideDirs: overrideDirs,
	}
}

// RegisterCustomFuncs adds custom template functions that will be available
// in all templates. Must be called before any Get/Render calls.
func (r *TemplateRegistry) RegisterCustomFuncs(funcs template.FuncMap) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range funcs {
		r.customFuncs[k] = v
	}
	// Invalidate cache since funcs changed
	r.cache = make(map[string]*template.Template)
}

// Get returns a parsed template by name. The name is a relative path
// without the .md.tpl extension, e.g. "base/identity", "agents/coder".
// External templates override embedded ones.
func (r *TemplateRegistry) Get(name string) (*template.Template, error) {
	r.mu.RLock()
	if tmpl, ok := r.cache[name]; ok {
		r.mu.RUnlock()
		return tmpl, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if tmpl, ok := r.cache[name]; ok {
		return tmpl, nil
	}

	content, err := r.loadTemplateContent(name)
	if err != nil {
		return nil, fmt.Errorf("template %q: %w", name, err)
	}

	tmpl, err := template.New(name).Funcs(r.defaultFuncs()).Funcs(r.customFuncs).Parse(content)
	if err != nil {
		return nil, fmt.Errorf("template %q parse error: %w", name, err)
	}

	r.cache[name] = tmpl
	return tmpl, nil
}

// Render renders a template by name with the given data and returns the output.
func (r *TemplateRegistry) Render(name string, data *PromptData) (string, error) {
	tmpl, err := r.Get(name)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("template %q render error: %w", name, err)
	}

	return buf.String(), nil
}

// Exists checks whether a template exists (either embedded or overridden).
func (r *TemplateRegistry) Exists(name string) bool {
	// Check external overrides first
	for _, dir := range r.overrideDirs {
		path := filepath.Join(dir, name+".md.tpl")
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	// Check embedded
	path := "templates/" + name + ".md.tpl"
	if _, err := embeddedTemplates.ReadFile(path); err == nil {
		return true
	}

	return false
}

// loadTemplateContent loads template content, checking external overrides first.
func (r *TemplateRegistry) loadTemplateContent(name string) (string, error) {
	// Check external override directories first (in order)
	for _, dir := range r.overrideDirs {
		path := filepath.Join(dir, name+".md.tpl")
		data, err := os.ReadFile(path)
		if err == nil {
			logging.Debug("Using external template override", "name", name, "path", path)
			return string(data), nil
		}
	}

	// Fall back to embedded templates
	path := "templates/" + name + ".md.tpl"
	data, err := embeddedTemplates.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("not found in embedded templates: %w", err)
	}

	return string(data), nil
}

// defaultFuncs returns the default template functions available in all templates.
func (r *TemplateRegistry) defaultFuncs() template.FuncMap {
	return template.FuncMap{
		"trimSpace": strings.TrimSpace,
		"join":      strings.Join,
		"contains":  strings.Contains,
		"lower":     strings.ToLower,
		"upper":     strings.ToUpper,
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"default": func(defaultVal, val string) string {
			if val == "" {
				return defaultVal
			}
			return val
		},
		"notEmpty": func(s string) bool {
			return strings.TrimSpace(s) != ""
		},
	}
}
