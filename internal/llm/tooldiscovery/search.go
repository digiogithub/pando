package tooldiscovery

import (
	"sort"
	"strings"
)

// SearchResult is a single match from a tool search query.
type SearchResult struct {
	CanonicalName string
	Aliases       []string
	ServerName    string
	Source        ToolSource
	Description   string
	Score         float64
}

// Search returns up to limit tools ranked by lexical relevance to query.
// Ranking uses a simple term-frequency + field-weight scoring over name,
// aliases, server name, description, and parameter names.
func (r *Registry) Search(query string, limit int) []SearchResult {
	if query == "" || limit <= 0 {
		return nil
	}

	tokens := tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	all := r.All()
	type candidate struct {
		entry Entry
		score float64
	}
	candidates := make([]candidate, 0, len(all))

	for _, e := range all {
		score := scoreEntry(e, tokens)
		if score > 0 {
			candidates = append(candidates, candidate{entry: e, score: score})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		// Tie-break: higher priority wins, then alphabetical
		if candidates[i].entry.Metadata.Priority != candidates[j].entry.Metadata.Priority {
			return candidates[i].entry.Metadata.Priority > candidates[j].entry.Metadata.Priority
		}
		return candidates[i].entry.Metadata.CanonicalName < candidates[j].entry.Metadata.CanonicalName
	})

	if limit > len(candidates) {
		limit = len(candidates)
	}

	results := make([]SearchResult, limit)
	for i := range results {
		e := candidates[i].entry
		info := e.Tool.Info()
		results[i] = SearchResult{
			CanonicalName: e.Metadata.CanonicalName,
			Aliases:       e.Metadata.Aliases,
			ServerName:    e.Metadata.ServerName,
			Source:        e.Metadata.Source,
			Description:   info.Description,
			Score:         candidates[i].score,
		}
	}
	return results
}

// fieldWeights controls how much each field contributes to the score.
const (
	weightName        = 4.0
	weightAlias       = 3.0
	weightServer      = 2.0
	weightDescription = 1.0
	weightParam       = 0.5
)

func scoreEntry(e Entry, tokens []string) float64 {
	info := e.Tool.Info()
	meta := e.Metadata

	nameTokens := tokenize(meta.CanonicalName)
	descTokens := tokenize(info.Description)

	var aliasTokens []string
	for _, a := range meta.Aliases {
		aliasTokens = append(aliasTokens, tokenize(a)...)
	}

	serverTokens := tokenize(meta.ServerName)

	var paramTokens []string
	for k := range info.Parameters {
		paramTokens = append(paramTokens, tokenize(k)...)
	}

	var total float64
	for _, t := range tokens {
		total += weightName * countMatches(nameTokens, t)
		total += weightAlias * countMatches(aliasTokens, t)
		total += weightServer * countMatches(serverTokens, t)
		total += weightDescription * countMatches(descTokens, t)
		total += weightParam * countMatches(paramTokens, t)
	}
	return total
}

func countMatches(haystack []string, needle string) float64 {
	var count float64
	for _, h := range haystack {
		if strings.EqualFold(h, needle) {
			count++
		} else if strings.Contains(strings.ToLower(h), strings.ToLower(needle)) {
			count += 0.5
		}
	}
	return count
}

// tokenize splits a string into lowercase tokens on non-alphanumeric characters.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var tokens []string
	start := -1
	for i, ch := range s {
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		if isAlnum {
			if start < 0 {
				start = i
			}
		} else {
			if start >= 0 {
				tokens = append(tokens, s[start:i])
				start = -1
			}
		}
	}
	if start >= 0 {
		tokens = append(tokens, s[start:])
	}
	return tokens
}
