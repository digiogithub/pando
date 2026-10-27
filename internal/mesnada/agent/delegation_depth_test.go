package agent

import (
	"strings"
	"testing"
)

func TestWithDelegationDepthReplacesInheritedValue(t *testing.T) {
	env := []string{"PATH=/bin", "PANDO_DELEGATION_DEPTH=1", "HOME=/h", "PANDO_DELEGATION_DEPTH=7"}
	got := withDelegationDepth(env, 2)
	count := 0
	for _, kv := range got {
		if strings.HasPrefix(kv, "PANDO_DELEGATION_DEPTH=") {
			count++
			if kv != "PANDO_DELEGATION_DEPTH=2" {
				t.Fatalf("depth entry = %q, want level 2", kv)
			}
		}
	}
	if count != 1 || len(got) != 3 {
		t.Fatalf("withDelegationDepth = %v, want exactly one depth entry plus PATH/HOME", got)
	}
	if env[1] != "PANDO_DELEGATION_DEPTH=1" {
		t.Fatal("input slice must not be modified")
	}
}

func TestBuildSubagentMCPConfigCarriesDepth(t *testing.T) {
	cfg := BuildSubagentMCPConfig("/usr/bin/pando", nil, false, 3)
	srv, ok := cfg.MCPServers["pando"]
	if !ok {
		t.Fatal("pando MCP server entry missing")
	}
	if srv.Env["PANDO_DELEGATION_DEPTH"] != "3" {
		t.Fatalf("pando MCP env = %v, want PANDO_DELEGATION_DEPTH=3", srv.Env)
	}
}
