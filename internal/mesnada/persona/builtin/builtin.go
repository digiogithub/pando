// Package builtin provides embedded built-in persona definitions.
package builtin

import "embed"

// FS contains all built-in persona markdown files.
//
//go:embed *.md
var FS embed.FS
