// Package i18n translates the native desktop UI with the Web UI's own
// catalogs (web-ui/src/i18n/locales, copied into ./locales; a test keeps the
// copies in sync). Keys are the i18next dotted paths ("nav.chat"),
// interpolation is i18next's {{name}}, and plurals use the _one/_other
// suffixes when a "count" argument is given. Missing keys fall back to
// English, then to the key itself.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

// Fallback is the language used for keys a catalog lacks.
const Fallback = "en"

var (
	loadOnce sync.Once
	catalogs map[string]map[string]string
	current  = Fallback
	mu       sync.RWMutex
)

func load() {
	catalogs = map[string]map[string]string{}
	entries, _ := localeFS.ReadDir("locales")
	for _, e := range entries {
		raw, err := localeFS.ReadFile(path.Join("locales", e.Name()))
		if err != nil {
			continue
		}
		var tree map[string]any
		if json.Unmarshal(raw, &tree) != nil {
			continue
		}
		flat := map[string]string{}
		flatten("", tree, flat)
		catalogs[strings.TrimSuffix(e.Name(), ".json")] = flat
	}
}

func flatten(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			flatten(key, child, out)
		}
	case string:
		out[prefix] = t
	default:
		out[prefix] = fmt.Sprint(t)
	}
}

// Languages returns the available language codes, sorted.
func Languages() []string {
	loadOnce.Do(load)
	out := make([]string, 0, len(catalogs))
	for k := range catalogs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// SetLanguage selects the language of T. A region (es-ES) falls back to its
// language; an unknown language selects English. It returns the language
// chosen.
func SetLanguage(lang string) string {
	loadOnce.Do(load)
	lang = strings.ToLower(strings.TrimSpace(lang))
	if _, ok := catalogs[lang]; !ok {
		if base, _, found := strings.Cut(strings.ReplaceAll(lang, "_", "-"), "-"); found {
			lang = base
		}
	}
	if _, ok := catalogs[lang]; !ok {
		lang = Fallback
	}
	mu.Lock()
	current = lang
	mu.Unlock()
	return lang
}

// Language returns the selected language.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T translates key in the selected language. args are name/value pairs
// for {{name}} placeholders: T("chat.files.changed", "count", 3).
func T(key string, args ...any) string {
	return TL(Language(), key, args...)
}

// TL translates key in lang.
func TL(lang, key string, args ...any) string {
	loadOnce.Do(load)
	vars := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		vars[fmt.Sprint(args[i])] = fmt.Sprint(args[i+1])
	}
	s, ok := lookup(lang, key, vars)
	if !ok {
		s, ok = lookup(Fallback, key, vars)
	}
	if !ok {
		return key
	}
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

func lookup(lang, key string, vars map[string]string) (string, bool) {
	cat := catalogs[lang]
	if cat == nil {
		return "", false
	}
	if n, ok := vars["count"]; ok {
		suffix := "_other"
		if n == "1" {
			suffix = "_one"
		}
		if s, ok := cat[key+suffix]; ok {
			return s, true
		}
	}
	s, ok := cat[key]
	return s, ok
}
