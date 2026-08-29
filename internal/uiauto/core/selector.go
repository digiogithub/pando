package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Combinator values describe the relationship between a step and the one
// before it in a Selector chain.
const (
	CombinatorDescendant = " "
	CombinatorChild      = ">"
)

// validAttrs is the set of attributes an AttrPredicate may target.
var validAttrs = map[string]bool{
	"name": true, "value": true, "description": true,
	"id": true, "role": true, "class": true,
}

// attrOps lists supported operators, longest first so the scanner prefers
// two-character operators over the bare "=".
var attrOps = []string{"^=", "$=", "*=", "~=", "="}

// AttrPredicate is a single `[attr op "value"]` filter on a SelectorStep.
type AttrPredicate struct {
	Attr  string
	Op    string
	Value string
}

// SelectorStep is one element of a Selector chain: an optional role token,
// zero or more attribute predicates, zero or more pseudo-class filters and
// an optional 1-indexed `nth` position filter.
type SelectorStep struct {
	// Combinator is the relationship to the previous step: "" for the
	// first step in the chain, CombinatorDescendant or CombinatorChild
	// otherwise.
	Combinator string
	// Role is the raw role token as written in the selector ("" or "*"
	// both mean "any role").
	Role    string
	Attrs   []AttrPredicate
	Pseudos []string
	// Nth is the 1-indexed sibling position filter, or 0 when unset. It
	// requires sibling context that Element alone does not carry, so
	// MatchesElement does not apply it; callers that walk a tree with
	// sibling information are expected to apply it themselves.
	Nth int
}

// Selector is a parsed chain of SelectorStep, e.g.
// `app[name="Chrome"] window[name="Settings"] > button[name="New Tab"]`.
type Selector struct {
	Steps []SelectorStep
}

// ParseSelector parses a selector string. It returns an INVALID_ARGS
// DesktopError on malformed input.
func ParseSelector(s string) (*Selector, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return nil, NewInvalidArgsError("selector must not be empty")
	}
	chunks, err := splitTopLevel(trimmed)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, NewInvalidArgsError("selector must not be empty")
	}

	var steps []SelectorStep
	pendingCombinator := ""
	sawStep := false
	for _, chunk := range chunks {
		if chunk == ">" {
			if !sawStep {
				return nil, NewInvalidArgsError("selector cannot start with '>'")
			}
			pendingCombinator = CombinatorChild
			continue
		}
		step, err := parseStep(chunk)
		if err != nil {
			return nil, err
		}
		if sawStep {
			if pendingCombinator == "" {
				pendingCombinator = CombinatorDescendant
			}
			step.Combinator = pendingCombinator
		} else {
			step.Combinator = ""
		}
		pendingCombinator = ""
		steps = append(steps, step)
		sawStep = true
	}
	if pendingCombinator == CombinatorChild {
		return nil, NewInvalidArgsError("selector cannot end with '>'")
	}
	if len(steps) == 0 {
		return nil, NewInvalidArgsError("selector must contain at least one step")
	}
	return &Selector{Steps: steps}, nil
}

// splitTopLevel splits s on whitespace and on standalone '>' tokens,
// treating '>' glued to a step (no surrounding space) as its own token too,
// while never splitting inside a double-quoted value.
func splitTopLevel(s string) ([]string, error) {
	var chunks []string
	var cur strings.Builder
	inQuotes := false
	flush := func() {
		if cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			cur.WriteRune(c)
		case inQuotes:
			cur.WriteRune(c)
			if c == '\\' && i+1 < len(runes) {
				i++
				cur.WriteRune(runes[i])
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			flush()
		case c == '>':
			flush()
			chunks = append(chunks, ">")
		default:
			cur.WriteRune(c)
		}
	}
	if inQuotes {
		return nil, NewInvalidArgsError("unterminated quoted string in selector")
	}
	flush()
	return chunks, nil
}

// parseStep parses a single step chunk (role token + brackets + pseudos),
// or the bare-quoted-text shorthand.
func parseStep(chunk string) (SelectorStep, error) {
	if chunk == "" {
		return SelectorStep{}, NewInvalidArgsError("empty selector step")
	}
	// Bare quoted shorthand: the whole chunk is a single quoted string.
	if chunk[0] == '"' {
		val, rest, err := readQuoted(chunk)
		if err != nil {
			return SelectorStep{}, err
		}
		if rest != "" {
			return SelectorStep{}, NewInvalidArgsError(fmt.Sprintf("unexpected trailing content after quoted shorthand: %q", chunk))
		}
		return SelectorStep{
			Attrs: []AttrPredicate{{Attr: "name", Op: "=", Value: val}},
		}, nil
	}

	i := 0
	// Role token: identifier chars or '*'.
	start := i
	for i < len(chunk) && chunk[i] != '[' && chunk[i] != ':' {
		i++
	}
	role := strings.TrimSpace(chunk[start:i])
	if role == "" {
		role = "*"
	} else if !isValidRoleToken(role) {
		return SelectorStep{}, NewInvalidArgsError(fmt.Sprintf("invalid role token %q", role))
	}

	step := SelectorStep{Role: role}
	for i < len(chunk) {
		switch chunk[i] {
		case '[':
			end := strings.IndexByte(chunk[i:], ']')
			if end < 0 {
				return SelectorStep{}, NewInvalidArgsError(fmt.Sprintf("unterminated '[' in selector step %q", chunk))
			}
			pred := chunk[i+1 : i+end]
			if err := applyPredicate(&step, pred); err != nil {
				return SelectorStep{}, err
			}
			i += end + 1
		case ':':
			j := i + 1
			for j < len(chunk) && chunk[j] != ':' && chunk[j] != '[' {
				j++
			}
			pseudo := chunk[i+1 : j]
			if !isValidPseudo(pseudo) {
				return SelectorStep{}, NewInvalidArgsError(fmt.Sprintf("unknown pseudo filter %q", pseudo))
			}
			step.Pseudos = append(step.Pseudos, pseudo)
			i = j
		default:
			return SelectorStep{}, NewInvalidArgsError(fmt.Sprintf("unexpected character %q in selector step %q", string(chunk[i]), chunk))
		}
	}
	return step, nil
}

func isValidRoleToken(s string) bool {
	if s == "*" {
		return true
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func isValidPseudo(p string) bool {
	switch p {
	case "visible", "enabled", "focused", "disabled", "hidden":
		return true
	default:
		return false
	}
}

// applyPredicate parses the content of a single `[...]` group and applies
// it to step: either `nth=N` or `attr op "value"`.
func applyPredicate(step *SelectorStep, pred string) error {
	pred = strings.TrimSpace(pred)
	if pred == "" {
		return NewInvalidArgsError("empty '[]' predicate in selector")
	}
	if strings.HasPrefix(pred, "nth=") {
		numStr := strings.TrimSpace(strings.TrimPrefix(pred, "nth="))
		n, err := strconv.Atoi(numStr)
		if err != nil || n < 1 {
			return NewInvalidArgsError(fmt.Sprintf("invalid nth value %q, must be a positive integer", numStr))
		}
		step.Nth = n
		return nil
	}

	// attrOps is ordered longest-operator-first, so scanning for the
	// earliest occurrence of any candidate and keeping the first hit
	// naturally prefers e.g. "^=" over a bare "=" at the same position.
	var op string
	opIdx := -1
	for _, candidate := range attrOps {
		idx := strings.Index(pred, candidate)
		if idx < 0 {
			continue
		}
		if opIdx == -1 || idx < opIdx {
			opIdx = idx
			op = candidate
		}
	}
	if opIdx < 0 {
		return NewInvalidArgsError(fmt.Sprintf("missing operator in predicate %q", pred))
	}
	attr := strings.TrimSpace(pred[:opIdx])
	if !validAttrs[attr] {
		return NewInvalidArgsError(fmt.Sprintf("unknown attribute %q; expected one of name,value,description,id,role,class", attr))
	}
	rawValue := strings.TrimSpace(pred[opIdx+len(op):])
	value, rest, err := readQuoted(rawValue)
	if err != nil {
		return err
	}
	if rest != "" {
		return NewInvalidArgsError(fmt.Sprintf("unexpected trailing content in predicate value %q", rawValue))
	}
	step.Attrs = append(step.Attrs, AttrPredicate{Attr: attr, Op: op, Value: value})
	return nil
}

// readQuoted reads a leading double-quoted string (supporting \" and \\
// escapes) from s and returns its unescaped value plus whatever follows
// the closing quote.
func readQuoted(s string) (value string, rest string, err error) {
	if len(s) == 0 || s[0] != '"' {
		return "", "", NewInvalidArgsError(fmt.Sprintf("expected a quoted string, got %q", s))
	}
	var sb strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			sb.WriteByte(s[i+1])
			i += 2
			continue
		}
		if c == '"' {
			return sb.String(), s[i+1:], nil
		}
		sb.WriteByte(c)
		i++
	}
	return "", "", NewInvalidArgsError(fmt.Sprintf("unterminated quoted string %q", s))
}

// attrValue extracts the value of a given attribute name from el.
func attrValue(el *Element, attr string) string {
	switch attr {
	case "name":
		return el.Name
	case "value":
		return el.Value
	case "description":
		return el.Description
	case "id":
		return string(el.ID)
	case "role":
		return string(el.Role)
	case "class":
		if el.Native.Data != nil {
			if v, ok := el.Native.Data["class"]; ok {
				if s, ok := v.(string); ok {
					return s
				}
			}
		}
		return ""
	default:
		return ""
	}
}

// matchOp applies a single attribute operator.
func matchOp(op, actual, expected string) bool {
	switch op {
	case "=":
		return actual == expected
	case "^=":
		return strings.HasPrefix(actual, expected)
	case "$=":
		return strings.HasSuffix(actual, expected)
	case "*=":
		return strings.Contains(actual, expected)
	case "~=":
		re, err := regexp.Compile(expected)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	default:
		return false
	}
}

// MatchesElement reports whether el satisfies this step's role, attribute
// and pseudo-class predicates. The Nth predicate, if set, is not evaluated
// here since it requires sibling context; see SelectorStep.Nth.
func (step SelectorStep) MatchesElement(el *Element) bool {
	if el == nil {
		return false
	}
	if step.Role != "" && step.Role != "*" && !el.Role.Matches(step.Role) {
		return false
	}
	for _, pred := range step.Attrs {
		if !matchOp(pred.Op, attrValue(el, pred.Attr), pred.Value) {
			return false
		}
	}
	for _, pseudo := range step.Pseudos {
		switch pseudo {
		case "visible":
			if !el.Visible {
				return false
			}
		case "enabled":
			if !el.Enabled {
				return false
			}
		case "focused":
			if !el.Focused {
				return false
			}
		case "disabled":
			if el.Enabled {
				return false
			}
		case "hidden":
			if el.Visible {
				return false
			}
		}
	}
	return true
}

// quoteValue renders v as a double-quoted string, escaping backslashes and
// quotes.
func quoteValue(v string) string {
	v = strings.ReplaceAll(v, "\\", "\\\\")
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

// String renders the step back to selector syntax (without its leading
// combinator, which Selector.String prints between steps).
func (step SelectorStep) String() string {
	var sb strings.Builder
	if step.Role != "" && step.Role != "*" {
		sb.WriteString(step.Role)
	}
	for _, pred := range step.Attrs {
		sb.WriteString("[")
		sb.WriteString(pred.Attr)
		sb.WriteString(pred.Op)
		sb.WriteString(quoteValue(pred.Value))
		sb.WriteString("]")
	}
	if step.Nth > 0 {
		sb.WriteString(fmt.Sprintf("[nth=%d]", step.Nth))
	}
	for _, pseudo := range step.Pseudos {
		sb.WriteString(":")
		sb.WriteString(pseudo)
	}
	out := sb.String()
	if out == "" {
		return "*"
	}
	return out
}

// String renders the Selector back to its canonical selector syntax.
func (s *Selector) String() string {
	if s == nil || len(s.Steps) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, step := range s.Steps {
		if i > 0 {
			if step.Combinator == CombinatorChild {
				sb.WriteString(" > ")
			} else {
				sb.WriteString(" ")
			}
		}
		sb.WriteString(step.String())
	}
	return sb.String()
}
