package config

import "testing"

func TestPonytailDefaultMode(t *testing.T) {
	t.Setenv("PANDO_PONYTAIL_DEFAULT_MODE", "")

	cases := []struct {
		field string
		want  string
	}{
		{"", ""},
		{"off", ""},
		{"bogus", ""},
		{"lite", "lite"},
		{"FULL", "full"},
		{" ultra ", "ultra"},
	}
	for _, tc := range cases {
		c := &Config{Ponytail: PonytailConfig{DefaultMode: tc.field}}
		if got := c.PonytailDefaultMode(); got != tc.want {
			t.Errorf("DefaultMode field %q = %q, want %q", tc.field, got, tc.want)
		}
	}

	// Env override takes precedence over the config field.
	t.Setenv("PANDO_PONYTAIL_DEFAULT_MODE", "ultra")
	c := &Config{Ponytail: PonytailConfig{DefaultMode: "lite"}}
	if got := c.PonytailDefaultMode(); got != "ultra" {
		t.Errorf("env override = %q, want ultra", got)
	}
}
