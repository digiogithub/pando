package outputfilter

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// run applies the 8-stage pipeline to output. The bool return is false only on
// an internal inconsistency (defensive); callers treat false as "fall back to
// raw output". A normal empty result after filtering returns ("" or on_empty, true).
func (f *Filter) run(output string) (string, bool) {
	s := output

	// Stage 1: strip ANSI escape sequences.
	if f.StripANSI {
		s = ansi.Strip(s)
	}

	// Stage 2: chained per-line regex substitutions.
	if len(f.Replace) > 0 {
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			for _, rule := range f.Replace {
				if rule.re != nil {
					line = rule.re.ReplaceAllString(line, rule.Replacement)
				}
			}
			lines[i] = line
		}
		s = strings.Join(lines, "\n")
	}

	// Stage 3: whole-blob short-circuit.
	for _, m := range f.MatchOutput {
		if m.re == nil || !m.re.MatchString(s) {
			continue
		}
		if m.unlessRe != nil && m.unlessRe.MatchString(s) {
			continue
		}
		return m.Message, true
	}

	// Stage 4: line filtering (strip OR keep, mutually exclusive).
	if len(f.stripRes) > 0 || len(f.keepRes) > 0 {
		lines := strings.Split(s, "\n")
		kept := make([]string, 0, len(lines))
		for _, line := range lines {
			if len(f.keepRes) > 0 {
				if anyMatch(f.keepRes, line) {
					kept = append(kept, line)
				}
				continue
			}
			if !anyMatch(f.stripRes, line) {
				kept = append(kept, line)
			}
		}
		s = strings.Join(kept, "\n")
	}

	// Stage 5: per-line length cap.
	if f.TruncateLinesAt > 0 {
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if len(line) > f.TruncateLinesAt {
				lines[i] = line[:f.TruncateLinesAt] + "…"
			}
		}
		s = strings.Join(lines, "\n")
	}

	// Stage 6: head/tail line windows.
	if f.HeadLines > 0 {
		s = headLines(s, f.HeadLines)
	}
	if f.TailLines > 0 {
		s = tailLines(s, f.TailLines)
	}

	// Stage 7: absolute max-lines cap.
	if f.MaxLines > 0 {
		s = capLines(s, f.MaxLines)
	}

	// Stage 8: on_empty fallback.
	if strings.TrimSpace(s) == "" && f.OnEmpty != "" {
		return f.OnEmpty, true
	}

	return s, true
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, r := range res {
		if r != nil && r.MatchString(s) {
			return true
		}
	}
	return false
}

func headLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return s
	}
	omitted := len(lines) - n
	return strings.Join(lines[:n], "\n") +
		"\n… [" + strconv.Itoa(omitted) + " more lines omitted]"
}

func tailLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return s
	}
	omitted := len(lines) - n
	return "… [" + strconv.Itoa(omitted) + " earlier lines omitted]\n" +
		strings.Join(lines[len(lines)-n:], "\n")
}

func capLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if n >= len(lines) {
		return s
	}
	omitted := len(lines) - n
	return strings.Join(lines[:n], "\n") +
		"\n… [" + strconv.Itoa(omitted) + " more lines omitted]"
}
