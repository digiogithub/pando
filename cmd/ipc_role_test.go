package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestEntrypointsPassIPCRoleToApp guards P3 of
// pando/plans/mcp_server_ipc_bootstrap.md: every entrypoint that bootstraps IPC
// tells app.New its role, so only the primary starts the primary-only
// background services, and cron is started once (by app.New), not again by ACP.
func TestEntrypointsPassIPCRoleToApp(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(b)
	}
	ipcRole := regexp.MustCompile(`IPCRole:\s+rt\.Role`)

	root := read("root.go")
	if n := len(ipcRole.FindAllString(root, -1)); n != 2 {
		t.Errorf("root.go passes IPCRole: rt.Role %d times, want 2 (TUI and ACP)", n)
	}
	if strings.Contains(root, "CronService.Start(") {
		t.Error("root.go starts the CronService; app.New starts it once, on the IPC primary only")
	}

	if !ipcRole.MatchString(read("mcp_server.go")) {
		t.Error("mcp_server.go does not pass IPCRole: rt.Role to app.New")
	}

	// serve/desktop/app build the App through api.NewServer, which forwards
	// ServerConfig.Role as AppOptions.IPCRole.
	for _, file := range []string{"serve.go", "desktop.go", "app.go"} {
		if !strings.Contains(read(file), "string(rt.Role)") {
			t.Errorf("%s does not pass rt.Role to api.NewServer", file)
		}
	}
	if !regexp.MustCompile(`IPCRole:\s+ipcruntime\.Role\(cfg\.Role\)`).MatchString(read("../internal/api/server.go")) {
		t.Error("api.NewServer does not forward ServerConfig.Role as AppOptions.IPCRole")
	}
}
