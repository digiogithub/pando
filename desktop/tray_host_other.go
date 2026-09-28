//go:build !linux && !darwin

package main

// trayHostAvailable is always true outside Linux: the Windows notification
// area is always present.
func trayHostAvailable() bool { return true }
