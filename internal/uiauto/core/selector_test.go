package core

import (
	"testing"
)

func TestParseSelector_Valid(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"simple role", "button"},
		{"wildcard", "*"},
		{"role with name eq", `button[name="Save"]`},
		{"prefix op", `textfield[name^="Search"]`},
		{"suffix op", `textfield[name$="Box"]`},
		{"contains op", `textfield[name*="ear"]`},
		{"regex op", `textfield[name~="^Sea.*"]`},
		{"pseudo visible", `button[name="Save"]:visible`},
		{"multiple pseudos", `button:visible:enabled`},
		{"nth predicate", `listitem[nth=2]`},
		{"descendant chain", `app[name="Chrome"] window[name="Settings"] button[name="New Tab"]`},
		{"child combinator", `group > button[name="OK"]`},
		{"bare quoted shorthand", `"Save"`},
		{"class attr", `button[class="primary"]`},
		{"id attr", `button[id="e1"]`},
		{"description attr", `image[description="logo"]`},
		{"role attr predicate", `*[role="button"]`},
		{"mixed chain with child then descendant", `window[name="Settings"] > group button[name="Save"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := ParseSelector(tc.input)
			if err != nil {
				t.Fatalf("ParseSelector(%q) unexpected error: %v", tc.input, err)
			}
			if len(sel.Steps) == 0 {
				t.Fatalf("ParseSelector(%q) produced no steps", tc.input)
			}
		})
	}
}

func TestParseSelector_Invalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		">",
		"button >",
		"> button",
		`button[name="unterminated`,
		`button[bogus="x"]`,
		`button[name?"x"]`,
		`button[nth=0]`,
		`button[nth=abc]`,
		`button:unknownpseudo`,
		`bad-role!`,
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := ParseSelector(input)
			if err == nil {
				t.Fatalf("ParseSelector(%q) expected error, got nil", input)
			}
			de, ok := AsDesktopError(err)
			if !ok {
				t.Fatalf("ParseSelector(%q) error is not a DesktopError: %v", input, err)
			}
			if de.Code != ErrInvalidArgs {
				t.Fatalf("ParseSelector(%q) expected INVALID_ARGS, got %s", input, de.Code)
			}
		})
	}
}

func TestSelector_StringRoundTrip(t *testing.T) {
	inputs := []string{
		`button[name="Save"]`,
		`textfield[name^="Search"]:visible`,
		`app[name="Chrome"] window[name="Settings"] > button[name="New Tab"]`,
		`listitem[nth=2]`,
		`button:visible:enabled`,
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			sel, err := ParseSelector(input)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			rendered := sel.String()
			sel2, err := ParseSelector(rendered)
			if err != nil {
				t.Fatalf("re-parse of rendered %q failed: %v", rendered, err)
			}
			if sel2.String() != rendered {
				t.Fatalf("round trip mismatch: %q vs %q", rendered, sel2.String())
			}
		})
	}
}

func TestSelectorStep_MatchesElement_Operators(t *testing.T) {
	el := &Element{
		Role:        RoleButton,
		Name:        "Search box",
		Value:       "hello",
		Description: "a search field",
		Enabled:     true,
		Visible:     true,
		Focused:     false,
	}

	cases := []struct {
		name     string
		selector string
		want     bool
	}{
		{"role match", "button", true},
		{"role mismatch", "checkbox", false},
		{"wildcard", "*", true},
		{"eq match", `button[name="Search box"]`, true},
		{"eq mismatch", `button[name="Other"]`, false},
		{"prefix match", `button[name^="Search"]`, true},
		{"prefix mismatch", `button[name^="box"]`, false},
		{"suffix match", `button[name$="box"]`, true},
		{"suffix mismatch", `button[name$="Search"]`, false},
		{"contains match", `button[name*="ch b"]`, true},
		{"contains mismatch", `button[name*="zzz"]`, false},
		{"regex match", `button[name~="^Search.*x$"]`, true},
		{"regex mismatch", `button[name~="^zzz$"]`, false},
		{"description match", `button[description="a search field"]`, true},
		{"value match", `button[value="hello"]`, true},
		{"pseudo visible true", `button:visible`, true},
		{"pseudo enabled true", `button:enabled`, true},
		{"pseudo focused false", `button:focused`, false},
		{"pseudo disabled false", `button:disabled`, false},
		{"pseudo hidden false", `button:hidden`, false},
		{"alias textbox->textfield mismatch on button", `textbox[name="Search box"]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := ParseSelector(tc.selector)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := sel.Steps[0].MatchesElement(el)
			if got != tc.want {
				t.Fatalf("selector %q against element: got %v, want %v", tc.selector, got, tc.want)
			}
		})
	}
}

func TestSelectorStep_MatchesElement_RoleAlias(t *testing.T) {
	el := &Element{Role: RoleTextField, Name: "Query", Enabled: true, Visible: true}
	sel, err := ParseSelector(`textbox[name="Query"]`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !sel.Steps[0].MatchesElement(el) {
		t.Fatalf("expected textbox alias to match textfield role")
	}
}

func TestParseSelector_BareQuotedShorthand(t *testing.T) {
	sel, err := ParseSelector(`"Save"`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	step := sel.Steps[0]
	if len(step.Attrs) != 1 || step.Attrs[0].Attr != "name" || step.Attrs[0].Op != "=" || step.Attrs[0].Value != "Save" {
		t.Fatalf("unexpected shorthand parse result: %+v", step)
	}
	el := &Element{Role: RoleHeading, Name: "Save", Visible: true, Enabled: true}
	if !step.MatchesElement(el) {
		t.Fatalf("expected shorthand to match any role with name == Save")
	}
	el2 := &Element{Role: RoleButton, Name: "Cancel", Visible: true, Enabled: true}
	if step.MatchesElement(el2) {
		t.Fatalf("expected shorthand not to match a differently named element")
	}
}

func TestParseSelector_Combinators(t *testing.T) {
	sel, err := ParseSelector(`window[name="Settings"] > button[name="Save"] group`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(sel.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(sel.Steps))
	}
	if sel.Steps[0].Combinator != "" {
		t.Fatalf("first step should have empty combinator, got %q", sel.Steps[0].Combinator)
	}
	if sel.Steps[1].Combinator != CombinatorChild {
		t.Fatalf("second step should have child combinator, got %q", sel.Steps[1].Combinator)
	}
	if sel.Steps[2].Combinator != CombinatorDescendant {
		t.Fatalf("third step should have descendant combinator, got %q", sel.Steps[2].Combinator)
	}
}

func TestParseSelector_Nth(t *testing.T) {
	sel, err := ParseSelector(`listitem[nth=3]`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if sel.Steps[0].Nth != 3 {
		t.Fatalf("expected Nth=3, got %d", sel.Steps[0].Nth)
	}
}
