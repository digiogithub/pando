package rag

import (
	"context"
	"regexp"
	"strings"
	"unicode"
)

// EnrichmentPlan holds the per-source queries and result limits derived from a raw user prompt.
// Instead of sending the raw prompt to every backend, each source receives a targeted query.
type EnrichmentPlan struct {
	// SemanticQuery is a cleaned query suitable for KB semantic search.
	SemanticQuery string
	// CodeQuery is a keyword-focused query for code hybrid search.
	CodeQuery string
	// EventsQuery is a brief topical query for past-event search.
	EventsQuery string
	// KBResults overrides the default KB result count (0 = use default).
	KBResults int
	// CodeResults overrides the default code result count (0 = use default).
	CodeResults int
	// EventsResults overrides the default events result count (0 = use default).
	EventsResults int
}

// QueryPlanner derives an EnrichmentPlan from a raw user prompt.
type QueryPlanner interface {
	Plan(ctx context.Context, prompt string) (*EnrichmentPlan, error)
}

// PlannerProvider is a minimal interface for LLM providers used by LLMPlanner.
// It avoids importing llm/provider (which would create an import cycle via llm/tools→rag).
// Implementations live in app/ where both sides can be imported safely.
type PlannerProvider interface {
	// SendRequest sends a simple system+user message and returns the text response.
	SendRequest(ctx context.Context, systemPrompt, userPrompt string) (string, error)
	// ProviderName returns the model provider name (used to select the system prompt variant).
	ProviderName() string
}

// -- heuristic planner -------------------------------------------------------

// HeuristicPlanner derives queries without calling an LLM.
// It applies a series of text-cleaning rules to remove imperative/meta language
// and preserve technical terms.
type HeuristicPlanner struct{}

// Plan implements QueryPlanner using purely local heuristics.
func (p *HeuristicPlanner) Plan(_ context.Context, prompt string) (*EnrichmentPlan, error) {
	cleaned := cleanPrompt(prompt)

	// Code query: preserve identifiers and technical terms, keep it concise.
	codeQ := extractCodeTerms(prompt)
	if codeQ == "" {
		codeQ = cleaned
	}

	// Events query: short conceptual phrase.
	eventsQ := firstSentence(cleaned, 80)

	return &EnrichmentPlan{
		SemanticQuery: cleaned,
		CodeQuery:     codeQ,
		EventsQuery:   eventsQ,
	}, nil
}

// -- helpers -----------------------------------------------------------------

// imperativeVerbs are common Spanish/English action prefixes that add noise to semantic search.
var imperativeVerbs = []string{
	// Spanish
	"analiza", "revisa", "implementa", "crea", "añade", "agrega", "elimina",
	"modifica", "cambia", "refactoriza", "explica", "describe", "muestra",
	"genera", "ejecuta", "comprueba", "verifica", "ayúdame", "ayudame",
	"quiero que", "necesito que", "por favor", "haz que", "asegúrate",
	"asegurate", "actualiza", "busca",
	// English
	"analyze", "review", "implement", "create", "add", "remove", "delete",
	"modify", "change", "refactor", "explain", "describe", "show", "generate",
	"run", "check", "verify", "help me", "i want", "i need", "please",
	"make sure", "ensure", "update", "find", "look at",
}

// reMeta matches meta-phrases at the beginning of a sentence.
var reMeta = buildMetaRegex()

func buildMetaRegex() *regexp.Regexp {
	parts := make([]string, 0, len(imperativeVerbs))
	for _, v := range imperativeVerbs {
		parts = append(parts, regexp.QuoteMeta(v))
	}
	// Match at start of string or after punctuation/newline
	return regexp.MustCompile(`(?i)^(?:` + strings.Join(parts, "|") + `)\s+`)
}

// cleanPrompt removes imperative prefixes, shortens to the first meaningful content,
// and collapses whitespace.
func cleanPrompt(prompt string) string {
	// Take only the first 500 chars to avoid embedding entire long prompts.
	if len(prompt) > 500 {
		prompt = prompt[:500]
	}

	// Remove meta phrases from the start.
	prompt = reMeta.ReplaceAllString(strings.TrimSpace(prompt), "")

	// Collapse newlines and extra spaces.
	prompt = strings.Join(strings.Fields(prompt), " ")

	// Trim to 300 chars for the semantic query.
	if len(prompt) > 300 {
		prompt = prompt[:300]
	}

	return strings.TrimSpace(prompt)
}

// reIdentifier matches Go/general-purpose identifiers (CamelCase or snake_case).
var reIdentifier = regexp.MustCompile(`[A-Z][a-zA-Z0-9]+[a-zA-Z0-9]|[a-z][a-zA-Z0-9]*[A-Z][a-zA-Z0-9]*|[a-z_][a-z_0-9]{2,}`)

// reQuoted matches text inside backticks or quotes.
var reQuoted = regexp.MustCompile("(?:^|\\s)`([^`]+)`")

// extractCodeTerms collects identifiers, quoted tokens, and capitalized technical
// words from the prompt to build a code-search-friendly query.
func extractCodeTerms(prompt string) string {
	var terms []string
	seen := map[string]bool{}

	add := func(t string) {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}

	// Quoted / backtick tokens first (highest precision).
	for _, m := range reQuoted.FindAllStringSubmatch(prompt, -1) {
		if len(m) > 1 {
			add(m[1])
		}
	}

	// CamelCase / snake_case identifiers.
	for _, m := range reIdentifier.FindAllString(prompt, -1) {
		add(m)
	}

	// Capitalized words (likely proper nouns / product names).
	for _, word := range strings.Fields(prompt) {
		word = strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if len(word) > 2 && unicode.IsUpper(rune(word[0])) {
			add(word)
		}
	}

	if len(terms) == 0 {
		return ""
	}

	q := strings.Join(terms, " ")
	if len(q) > 200 {
		q = q[:200]
	}
	return q
}

// firstSentence returns up to maxLen chars ending on a word boundary, preferring
// the first sentence of the text.
func firstSentence(text string, maxLen int) string {
	// Try to end at a sentence boundary.
	for _, sep := range []string{".", "?", "!", "\n"} {
		if idx := strings.Index(text, sep); idx > 0 && idx < maxLen {
			return strings.TrimSpace(text[:idx])
		}
	}
	if len(text) <= maxLen {
		return text
	}
	// Break at last space before maxLen.
	sub := text[:maxLen]
	if i := strings.LastIndex(sub, " "); i > 0 {
		return strings.TrimSpace(sub[:i])
	}
	return strings.TrimSpace(sub)
}
