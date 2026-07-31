package config

import "testing"

func TestParseHeaderPairs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty", input: "   ", want: nil},
		{
			name:  "value with spaces",
			input: "Authorization: Bearer abc def",
			want:  map[string]string{"Authorization": "Bearer abc def"},
		},
		{
			name:  "comma separated",
			input: "Authorization: Bearer tok, X-Figma-Token: abc 123",
			want:  map[string]string{"Authorization": "Bearer tok", "X-Figma-Token": "abc 123"},
		},
		{
			name:  "newline separated",
			input: "Authorization: Bearer tok\nX-Trace: 1",
			want:  map[string]string{"Authorization": "Bearer tok", "X-Trace": "1"},
		},
		{
			name:  "legacy space separated",
			input: "A:1 B:2",
			want:  map[string]string{"A": "1", "B": "2"},
		},
		{name: "missing colon", input: "Authorization Bearer", wantErr: true},
		{name: "empty key", input: ": value", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseHeaderPairs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseHeaderPairs(%q) expected an error, got %#v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseHeaderPairs(%q) unexpected error: %v", tt.input, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseHeaderPairs(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("ParseHeaderPairs(%q)[%q] = %q, want %q", tt.input, k, got[k], v)
				}
			}
		})
	}
}

func TestFormatHeaderPairsRoundTrip(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer abc def", "X-Trace": "1"}
	formatted := FormatHeaderPairs(headers)
	if formatted != "Authorization: Bearer abc def, X-Trace: 1" {
		t.Fatalf("FormatHeaderPairs() = %q", formatted)
	}
	parsed, err := ParseHeaderPairs(formatted)
	if err != nil {
		t.Fatalf("round trip parse failed: %v", err)
	}
	for k, v := range headers {
		if parsed[k] != v {
			t.Fatalf("round trip [%q] = %q, want %q", k, parsed[k], v)
		}
	}
}

func TestParseEnvPairs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		wantErr bool
	}{
		{name: "empty", input: "  ", want: nil},
		{name: "single", input: "FIGMA_API_KEY=abc123", want: []string{"FIGMA_API_KEY=abc123"}},
		{name: "comma separated", input: "A=1, B=2", want: []string{"A=1", "B=2"}},
		{name: "legacy space separated", input: "A=1 B=2", want: []string{"A=1", "B=2"}},
		{name: "value with spaces", input: "GREETING=hello world", want: []string{"GREETING=hello world"}},
		{name: "missing equals", input: "JUST_A_KEY", wantErr: true},
		{name: "empty key", input: "=value", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnvPairs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseEnvPairs(%q) expected an error, got %#v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEnvPairs(%q) unexpected error: %v", tt.input, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ParseEnvPairs(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("ParseEnvPairs(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
