// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

//go:build !linux

package runtime

// procStateSupported reports whether processState can inspect process states on
// this platform. Only Linux (/proc/<pid>/stat) is supported today; everywhere
// else the suspended-primary guard is a documented no-op and killStalePrimary
// keeps its long-standing behaviour (see its doc comment).
const procStateSupported = false

// processState always reports "unknown" off Linux. macOS would need
// sysctl(KERN_PROC) and Windows a different model entirely; neither is wired
// up, and guessing would be worse than the honest fallback.
func processState(int) (byte, bool) { return 0, false }
