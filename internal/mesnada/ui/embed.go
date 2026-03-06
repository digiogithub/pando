package ui

import "embed"

// FS contains the UI assets embedded into the mesnada binary.
//
// Keeping the embed directive in the same folder as the assets avoids needing
// ".." paths (which go:embed disallows) and ensures the UI works regardless of
// the process working directory.
//
//go:embed index.html partials/*.html assets/*
var FS embed.FS
