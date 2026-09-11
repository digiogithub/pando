// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build linux

package runtime

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// procStateSupported reports whether processState can inspect process states on
// this platform. Linux exposes them through /proc/<pid>/stat.
const procStateSupported = true

// processState returns the single-letter process state of pid as the kernel
// reports it in field 3 of /proc/<pid>/stat:
//
//	R running, S sleeping (interruptible), D uninterruptible sleep (I/O),
//	T stopped by a job-control signal (SIGSTOP/SIGTSTP, or `kill -STOP`),
//	t stopped by a debugger (ptrace trap), Z zombie, X dead, I idle kernel task.
//
// ok is false when the state could not be read at all (the process is gone,
// /proc is not mounted, the format is unexpected); callers must then fall back
// to their platform-independent behaviour rather than assume anything.
func processState(pid int) (state byte, ok bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, false
	}
	// Field 2 (comm) is wrapped in parentheses and may itself contain spaces
	// and parentheses, so the fields after it are found from the LAST ')'.
	idx := bytes.LastIndexByte(data, ')')
	if idx < 0 || idx+2 >= len(data) {
		return 0, false
	}
	rest := strings.TrimLeft(string(data[idx+1:]), " ")
	if rest == "" {
		return 0, false
	}
	return rest[0], true
}
