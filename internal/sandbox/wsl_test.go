package sandbox

import "testing"

func TestIsWSLProcVersion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "wsl2",
			in:   "Linux version 5.15.90.1-microsoft-standard-WSL2 (oe-user@oe-host) (gcc ...) #1 SMP ...",
			want: true,
		},
		{
			name: "wsl1",
			in:   "Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com) (gcc ...) #488-Microsoft ...",
			want: true,
		},
		{
			name: "native linux",
			in:   "Linux version 6.8.0-45-generic (buildd@lcy02-amd64-119) (x86_64-linux-gnu-gcc ...) #45-Ubuntu ...",
			want: false,
		},
		{
			name: "empty",
			in:   "",
			want: false,
		},
		{
			name: "case insensitive",
			in:   "Linux version 5.10.0-MICROSOFT-standard-WSL2",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWSLProcVersion(tc.in); got != tc.want {
				t.Errorf("isWSLProcVersion(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestIsWSL_Smoke exercises the real filesystem path (no fakeable /proc on
// most CI runners): it must never panic or error, whatever the result on
// this host.
func TestIsWSL_Smoke(t *testing.T) {
	_ = IsWSL()
}
