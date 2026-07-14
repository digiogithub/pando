package caveman

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	cases := map[string]struct {
		want Mode
		ok   bool
	}{
		"lite":     {ModeLite, true},
		"FULL":     {ModeFull, true},
		" ultra ":  {ModeUltra, true},
		"Wenyan":   {ModeWenyan, true},
		"off":      {ModeOff, true},
		"stop":     {ModeOff, true},
		"normal":   {ModeOff, true},
		"disabled": {ModeOff, true},
		"":         {ModeOff, false},
		"bogus":    {ModeOff, false},
	}
	for in, exp := range cases {
		got, ok := ParseMode(in)
		if got != exp.want || ok != exp.ok {
			t.Errorf("ParseMode(%q) = (%v,%v), want (%v,%v)", in, got, ok, exp.want, exp.ok)
		}
	}
}

func TestIsActiveAndString(t *testing.T) {
	if ModeOff.IsActive() {
		t.Error("ModeOff must not be active")
	}
	if got := ModeOff.String(); got != "off" {
		t.Errorf("ModeOff.String() = %q, want \"off\"", got)
	}
	for _, m := range []Mode{ModeLite, ModeFull, ModeUltra, ModeWenyan} {
		if !m.IsActive() {
			t.Errorf("%v must be active", m)
		}
		if m.String() != string(m) {
			t.Errorf("%v.String() = %q", m, m.String())
		}
	}
}

func TestDescription(t *testing.T) {
	if got := Description(ModeOff); got != "Disabled." {
		t.Errorf("Description(off) = %q", got)
	}
	for _, m := range []Mode{ModeLite, ModeFull, ModeUltra, ModeWenyan} {
		if strings.TrimSpace(Description(m)) == "" {
			t.Errorf("Description(%v) is empty", m)
		}
	}
}

func TestInstructionsOffIsEmpty(t *testing.T) {
	if got := Instructions(ModeOff); got != "" {
		t.Errorf("Instructions(off) = %q, want empty", got)
	}
}

func TestInstructionsIncludesCommonAndLevel(t *testing.T) {
	for _, m := range []Mode{ModeLite, ModeFull, ModeUltra, ModeWenyan} {
		got := Instructions(m)
		if !strings.Contains(got, "Output brevity mode") {
			t.Errorf("Instructions(%v) missing common policy", m)
		}
		if !strings.Contains(got, "## Level: "+string(m)) {
			t.Errorf("Instructions(%v) missing its level snippet", m)
		}
	}
}

// The policy must never license skipping verification or mangling exact text:
// these guarantees are what makes the mode safe to enable globally.
func TestInstructionsPreserveSafetyGuarantees(t *testing.T) {
	got := Instructions(ModeUltra)
	for _, want := range []string{
		"Keep exact, never compress",
		"Keep doing, never skip",
		"error text",
		"skip verification",
		"user's language",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Instructions(ultra) missing guarantee %q", want)
		}
	}
}

func TestWenyanKeepsCodeVerbatim(t *testing.T) {
	got := Instructions(ModeWenyan)
	if !strings.Contains(got, "Classical Chinese") {
		t.Error("wenyan level must name Classical Chinese")
	}
	if !strings.Contains(got, "stay verbatim") {
		t.Error("wenyan level must keep code/commands verbatim")
	}
}
