package version

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "prefixed", input: "v1.2.3", want: "1.2.3", wantOK: true},
		{name: "bare", input: "1.2.3", want: "1.2.3", wantOK: true},
		{name: "unknown", input: "unknown", wantOK: false},
		{name: "devel", input: "(devel)", wantOK: false},
		{name: "dirty_suffix", input: "v1.2.3+dirty", want: "1.2.3", wantOK: true},
		{name: "empty", input: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Parse(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("Parse(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.String() != tt.want {
				t.Fatalf("Parse(%q) = %q, want %q", tt.input, got.String(), tt.want)
			}
		})
	}
}

func TestCanonical(t *testing.T) {
	original := Version
	defer func() { Version = original }()

	Version = "1.2.3"
	if got := Canonical(); got != "v1.2.3" {
		t.Fatalf("Canonical() = %q, want %q", got, "v1.2.3")
	}

	Version = "unknown"
	if got := Canonical(); got != "unknown" {
		t.Fatalf("Canonical() = %q, want %q", got, "unknown")
	}

	Version = "1.2.3+dirty"
	if got := Canonical(); got != "v1.2.3" {
		t.Fatalf("Canonical() = %q, want %q", got, "v1.2.3")
	}
}
