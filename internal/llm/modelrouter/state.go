package modelrouter

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const (
	// charsPerToken is the cheap token estimator ratio (conservative: real
	// tokenizers average 3.5-4 chars/token for English and code).
	charsPerToken = 3.5

	// Marker joins the head and the tail of a truncated text.
	truncationMarker = "\n[…]\n"

	// maxStateBytes keeps the whole request body far below the 64 KiB cap.
	maxStateBytes = 40 * 1024

	maxAttachmentNames      = 20
	maxAttachmentNameRunes  = 80
	maxHistoryEntryChars    = 300
	minRequestChars         = 64
	defaultBudgetTokens     = 2048
	headFractionNumerator   = 3
	headFractionDenominator = 5
)

// EstimateTokens is the cheap token estimator used by the router.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return int(float64(len(s))/charsPerToken) + 1
}

// SafetyMargin returns the number of tokens kept free below a budget for the
// model's own answer and estimator error.
func SafetyMargin(budgetTokens int) int {
	m := budgetTokens / 16
	if m < 48 {
		m = 48
	}
	return m
}

// state is the JSON document sent as the System One "state".
type state struct {
	Request          string   `json:"request"`
	PreviousRequests []string `json:"previous_requests,omitempty"`
	Attachments      []string `json:"attachments,omitempty"`
}

// BuildState renders the System One state for in as a JSON object string
// {"request", "previous_requests"?, "attachments"?}. budgetTokens is the token
// budget available for the state (the caller has already subtracted the
// question and criteria overhead); a safety margin is reserved on top of that
// so the estimated size of the result is at most budgetTokens minus
// SafetyMargin(budgetTokens). The prompt keeps its head and tail joined by an
// ellipsis marker; only the last historyPrompts previous prompts are included,
// each truncated harder; attachments are listed by file name only.
func BuildState(in Input, historyPrompts int, budgetTokens int) string {
	if budgetTokens <= 0 {
		budgetTokens = defaultBudgetTokens
	}
	target := budgetTokens - SafetyMargin(budgetTokens)
	maxBytes := int(float64(target) * charsPerToken)
	if maxBytes > maxStateBytes {
		maxBytes = maxStateBytes
	}
	if maxBytes < 2*minRequestChars {
		maxBytes = 2 * minRequestChars
	}

	st := state{}
	for _, n := range in.AttachmentNames {
		if len(st.Attachments) >= maxAttachmentNames {
			break
		}
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		st.Attachments = append(st.Attachments, truncateMiddle(n, maxAttachmentNameRunes))
	}

	history := lastN(in.History, historyPrompts)
	if len(history) > 0 {
		// History may use at most a quarter of the state, split evenly.
		per := maxBytes / 4 / len(history)
		if per > maxHistoryEntryChars {
			per = maxHistoryEntryChars
		}
		if per < 24 {
			per = 24
		}
		for _, h := range history {
			h = strings.TrimSpace(h)
			if h == "" {
				continue
			}
			st.PreviousRequests = append(st.PreviousRequests, truncateMiddle(h, per))
		}
	}

	prompt := strings.TrimSpace(in.Prompt)
	limit := maxBytes
	for {
		st.Request = truncateMiddle(prompt, limit)
		out := render(st)
		if len(out) <= maxBytes && EstimateTokens(out) <= target {
			return out
		}
		if limit <= minRequestChars {
			// Drop history, then attachments, before giving up.
			if len(st.PreviousRequests) > 0 {
				st.PreviousRequests = nil
				limit = maxBytes
				continue
			}
			if len(st.Attachments) > 0 {
				st.Attachments = nil
				limit = maxBytes
				continue
			}
			return out
		}
		limit = limit * 9 / 10
		if limit < minRequestChars {
			limit = minRequestChars
		}
	}
}

func render(st state) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(st)
	return strings.TrimRight(buf.String(), "\n")
}

func lastN(in []string, n int) []string {
	if n <= 0 || len(in) == 0 {
		return nil
	}
	if len(in) > n {
		in = in[len(in)-n:]
	}
	return in
}

// truncateMiddle shortens s to at most maxBytes bytes, keeping the head (60%)
// and the tail (40%) joined by the ellipsis marker. Cuts respect rune and line
// boundaries where cheap.
func truncateMiddle(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	avail := maxBytes - len(truncationMarker)
	if avail < 8 {
		avail = 8
	}
	headN := avail * headFractionNumerator / headFractionDenominator
	tailN := avail - headN
	head := s[:headN]
	for len(head) > 0 && !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	tail := s[len(s)-tailN:]
	for len(tail) > 0 && !utf8.ValidString(tail) {
		tail = tail[1:]
	}
	return strings.TrimRight(head, " \t") + truncationMarker + strings.TrimLeft(tail, " \t")
}
