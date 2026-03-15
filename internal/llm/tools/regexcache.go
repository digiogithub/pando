package tools

import (
	"regexp"
	"sync"
)

// regexCache caches compiled regular expressions to avoid recompilation
// of the same pattern across multiple tool calls within a session.
var regexCache sync.Map // map[string]*regexp.Regexp

// getOrCompileRegex returns a compiled regexp for the given pattern,
// using the cache if available.
func getOrCompileRegex(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, re)
	return re, nil
}

// ResetRegexCache clears all cached regular expressions.
// Should be called at session end to prevent memory leaks.
func ResetRegexCache() {
	regexCache.Range(func(key, _ any) bool {
		regexCache.Delete(key)
		return true
	})
}

// ResetAllCaches clears all session-scoped caches (file locks and regex cache).
// Call this at the end of each session to prevent memory leaks.
func ResetAllCaches() {
	resetFileLocks()
	ResetRegexCache()
}
