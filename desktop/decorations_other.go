//go:build !linux

package main

// suppressServerDecorations is only needed on Linux/Wayland; Windows and
// macOS honour the frameless option directly.
func suppressServerDecorations() {}
