package tools

import (
	"reflect"
	"testing"
)

// TestRemoteBrowserServeArgs verifies the "serve" argument list built for each
// CDP-server browser type, without launching any browser process.
func TestRemoteBrowserServeArgs(t *testing.T) {
	tests := []struct {
		name        string
		browserType string
		host        string
		port        int
		want        []string
	}{
		{
			name:        "obscura",
			browserType: "obscura",
			host:        "127.0.0.1",
			port:        9222,
			want:        []string{"serve", "--host", "127.0.0.1", "--port", "9222", "--allow-private-network", "--allow-file-access", "--quiet"},
		},
		{
			name:        "obscura alias casing",
			browserType: "Obscura-Browser",
			host:        "127.0.0.1",
			port:        9333,
			want:        []string{"serve", "--host", "127.0.0.1", "--port", "9333", "--allow-private-network", "--allow-file-access", "--quiet"},
		},
		{
			name:        "lightpanda",
			browserType: "lightpanda",
			host:        "127.0.0.1",
			port:        9222,
			want:        []string{"serve", "--host", "127.0.0.1", "--port", "9222"},
		},
		{
			name:        "lightpanda alias casing",
			browserType: "Light-Panda",
			host:        "127.0.0.1",
			port:        9444,
			want:        []string{"serve", "--host", "127.0.0.1", "--port", "9444"},
		},
		{
			name:        "unknown type falls back to lightpanda-style args",
			browserType: "some-future-cdp-browser",
			host:        "0.0.0.0",
			port:        7000,
			want:        []string{"serve", "--host", "0.0.0.0", "--port", "7000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteBrowserServeArgs(tt.browserType, tt.host, tt.port)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("remoteBrowserServeArgs(%q, %q, %d) = %v, want %v", tt.browserType, tt.host, tt.port, got, tt.want)
			}
		})
	}
}
