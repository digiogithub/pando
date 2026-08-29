package core

import "testing"

func TestNormalizeRole_AllPlatforms(t *testing.T) {
	cases := []struct {
		platform string
		raw      string
		want     Role
	}{
		// atspi
		{"atspi", "push button", RoleButton},
		{"atspi", "frame", RoleWindow},
		{"atspi", "check box", RoleCheckbox},
		{"atspi", "entry", RoleTextField},
		{"atspi", "list item", RoleListItem},
		{"atspi", "table cell", RoleCell},
		// uia
		{"uia", "button", RoleButton},
		{"uia", "edit", RoleTextField},
		{"uia", "tree item", RoleTreeItem},
		{"uia", "data grid", RoleTable},
		{"uia", "hyperlink", RoleLink},
		// ax
		{"ax", "button", RoleButton},
		{"ax", "textfield", RoleTextField},
		{"ax", "popupbutton", RoleComboBox},
		{"ax", "statictext", RoleText},
		{"ax", "outline", RoleTree},
		// cdp
		{"cdp", "textbox", RoleTextField},
		{"cdp", "webarea", RoleApplication},
		{"cdp", "listitem", RoleListItem},
		{"cdp", "columnheader", RoleCell},
	}
	for _, tc := range cases {
		t.Run(tc.platform+"/"+tc.raw, func(t *testing.T) {
			got := NormalizeRole(tc.platform, tc.raw)
			if got != tc.want {
				t.Fatalf("NormalizeRole(%q, %q) = %q, want %q", tc.platform, tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeRole_CaseInsensitivePlatform(t *testing.T) {
	got := NormalizeRole("ATSPI", "Push Button")
	if got != RoleButton {
		t.Fatalf("expected case-insensitive platform/raw lookup to yield button, got %q", got)
	}
}

func TestNormalizeRole_FallbackToCanonical(t *testing.T) {
	got := NormalizeRole("someunknownplatform", "button")
	if got != RoleButton {
		t.Fatalf("expected fallback lowercase match against canonical vocabulary, got %q", got)
	}
}

func TestNormalizeRole_FallbackUnknown(t *testing.T) {
	got := NormalizeRole("atspi", "some totally unrecognized role")
	if got != RoleUnknown {
		t.Fatalf("expected RoleUnknown for unrecognized role, got %q", got)
	}
}

func TestNormalizeRole_EmptyRaw(t *testing.T) {
	if got := NormalizeRole("atspi", ""); got != RoleUnknown {
		t.Fatalf("expected RoleUnknown for empty raw role, got %q", got)
	}
}

func TestRole_Matches(t *testing.T) {
	cases := []struct {
		role     Role
		selector string
		want     bool
	}{
		{RoleButton, "button", true},
		{RoleButton, "BUTTON", true},
		{RoleButton, "*", true},
		{RoleButton, "checkbox", false},
		{RoleTextField, "textbox", true},
		{RoleTextField, "edit", true},
		{RoleLink, "anchor", true},
		{RoleGroup, "grp", true},
		{RoleSeparator, "hr", true},
		{RoleButton, "unknownalias", false},
	}
	for _, tc := range cases {
		t.Run(string(tc.role)+"/"+tc.selector, func(t *testing.T) {
			if got := tc.role.Matches(tc.selector); got != tc.want {
				t.Fatalf("Role(%q).Matches(%q) = %v, want %v", tc.role, tc.selector, got, tc.want)
			}
		})
	}
}
