// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

// isolatedConfigForBridgeTest loads a fresh config for a throwaway project
// with an isolated $HOME, so resolveAcceptDelegations reads a config this test
// controls instead of the developer's real one.
func isolatedConfigForBridgeTest(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	project := t.TempDir()
	t.Chdir(project)
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
	if _, err := config.Load(project, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
}

// TestResolveAcceptDelegations covers wireOptions.AcceptDelegations: nil keeps
// the configured default (false out of the box), and a non-nil override wins
// regardless of that default. This is the exact mechanism P2's ephemeral
// mcp-server will use to refuse peer delegations unconditionally.
func TestResolveAcceptDelegations(t *testing.T) {
	isolatedConfigForBridgeTest(t)

	if got := resolveAcceptDelegations(nil); got != false {
		t.Fatalf("resolveAcceptDelegations(nil) = %v, want the config default (false)", got)
	}

	trueVal := true
	if got := resolveAcceptDelegations(&trueVal); got != true {
		t.Fatalf("resolveAcceptDelegations(&true) = %v, want true (override wins)", got)
	}

	falseVal := false
	if got := resolveAcceptDelegations(&falseVal); got != false {
		t.Fatalf("resolveAcceptDelegations(&false) = %v, want false (override wins)", got)
	}

	// Flip the persisted config default on and confirm the override still wins.
	cfg := config.Get()
	cfg.Mesnada.Delegation.AcceptDelegations = true
	if got := resolveAcceptDelegations(nil); got != true {
		t.Fatalf("resolveAcceptDelegations(nil) = %v, want the flipped config default (true)", got)
	}
	if got := resolveAcceptDelegations(&falseVal); got != false {
		t.Fatalf("resolveAcceptDelegations(&false) = %v, want false even though config default is now true", got)
	}
}
