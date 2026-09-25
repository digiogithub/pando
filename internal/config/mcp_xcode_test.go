package config

import "testing"

func TestMCPServer_IsXcodeMCPBridge(t *testing.T) {
	tests := []struct {
		name string
		srv  MCPServer
		want bool
	}{
		{
			name: "absolute path to mcpbridge",
			srv:  MCPServer{Type: MCPStdio, Command: "/Applications/Xcode-27.0.app/Contents/Developer/usr/bin/mcpbridge"},
			want: true,
		},
		{
			name: "absolute path, no explicit type (defaults to stdio)",
			srv:  MCPServer{Command: "/Applications/Xcode-27.0.app/Contents/Developer/usr/bin/mcpbridge"},
			want: true,
		},
		{
			name: "xcrun mcpbridge",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"mcpbridge"}},
			want: true,
		},
		{
			name: "xcrun --sdk macosx mcpbridge",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"--sdk", "macosx", "mcpbridge"}},
			want: true,
		},
		{
			name: "xcrun with an absolute mcpbridge arg",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"--sdk", "macosx", "/usr/bin/mcpbridge"}},
			want: true,
		},
		{
			name: "xcrun running something else",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"--sdk", "macosx", "swift"}},
			want: false,
		},
		{
			name: "xcrun with only flags, no positional arg",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"--version"}},
			want: false,
		},
		{
			name: "unrelated stdio command",
			srv:  MCPServer{Type: MCPStdio, Command: "npx", Args: []string{"some-mcp-server"}},
			want: false,
		},
		{
			name: "empty command",
			srv:  MCPServer{Type: MCPStdio, Command: ""},
			want: false,
		},
		{
			name: "non-stdio type (sse) is never the bridge even with the right command",
			srv:  MCPServer{Type: MCPSse, Command: "mcpbridge"},
			want: false,
		},
		{
			name: "non-stdio type (streamable-http) is never the bridge",
			srv:  MCPServer{Type: MCPStreamableHTTP, URL: "https://example.com/mcp"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.srv.IsXcodeMCPBridge(); got != tt.want {
				t.Errorf("IsXcodeMCPBridge() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMCPServer_SandboxExempt(t *testing.T) {
	tests := []struct {
		name string
		srv  MCPServer
		want bool
	}{
		{
			name: "neither flag nor mcpbridge",
			srv:  MCPServer{Type: MCPStdio, Command: "npx", Args: []string{"some-mcp-server"}},
			want: false,
		},
		{
			name: "explicit NoSandbox on an unrelated command",
			srv:  MCPServer{Type: MCPStdio, Command: "npx", Args: []string{"some-mcp-server"}, NoSandbox: true},
			want: true,
		},
		{
			name: "detected as Xcode's mcpbridge without NoSandbox set",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"mcpbridge"}},
			want: true,
		},
		{
			name: "mcpbridge with NoSandbox also set",
			srv:  MCPServer{Type: MCPStdio, Command: "xcrun", Args: []string{"mcpbridge"}, NoSandbox: true},
			want: true,
		},
		{
			name: "Sandbox=true alone does not defeat NoSandbox exemption",
			srv:  MCPServer{Type: MCPStdio, Command: "npx", Sandbox: true, NoSandbox: true},
			want: true,
		},
		{
			name: "Sandbox=true with no exemption is not exempt",
			srv:  MCPServer{Type: MCPStdio, Command: "npx", Sandbox: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.srv.SandboxExempt(); got != tt.want {
				t.Errorf("SandboxExempt() = %v, want %v", got, tt.want)
			}
		})
	}
}
