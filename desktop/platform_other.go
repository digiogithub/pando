//go:build !darwin

package main

import "github.com/digiogithub/pando/internal/desktop"

// installPlatformHooks has nothing to install outside macOS.
func installPlatformHooks(*desktop.App) {}
