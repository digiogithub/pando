package ponytail

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]struct {
		want Mode
		ok   bool
	}{
		"lite":    {ModeLite, true},
		"FULL":    {ModeFull, true},
		" ultra ": {ModeUltra, true},
		"off":     {ModeOff, true},
		"stop":    {ModeOff, true},
		"normal":  {ModeOff, true},
		"":        {ModeOff, true}, // empty trims to "" → matches "" off synonym? no
		"bogus":   {ModeOff, false},
	}
	for in, exp := range cases {
		// "" is not in the off-synonym list, so it is unrecognized.
		if strings.TrimSpace(in) == "" {
			exp.ok = false
		}
		got, ok := ParseMode(in)
		if got != exp.want || ok != exp.ok {
			t.Errorf("ParseMode(%q) = (%v,%v), want (%v,%v)", in, got, ok, exp.want, exp.ok)
		}
	}
}

func TestInstructionsOffIsEmpty(t *testing.T) {
	if got := Instructions(ModeOff); got != "" {
		t.Errorf("Instructions(off) = %q, want empty", got)
	}
}

func TestInstructionsIncludesCommonAndIntensity(t *testing.T) {
	for _, m := range []Mode{ModeLite, ModeFull, ModeUltra} {
		got := Instructions(m)
		if !strings.Contains(got, "lazy senior developer") {
			t.Errorf("Instructions(%s) missing common intro", m)
		}
		if !strings.Contains(got, "The Ladder") {
			t.Errorf("Instructions(%s) missing The Ladder", m)
		}
		marker := "## Intensity: " + string(m)
		if !strings.Contains(got, marker) {
			t.Errorf("Instructions(%s) missing %q", m, marker)
		}
		// Must not leak the other intensity snippets.
		for _, other := range []Mode{ModeLite, ModeFull, ModeUltra} {
			if other == m {
				continue
			}
			if strings.Contains(got, "## Intensity: "+string(other)) {
				t.Errorf("Instructions(%s) leaked intensity for %s", m, other)
			}
		}
	}
}
