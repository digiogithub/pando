//go:build !((darwin || linux || windows) && (amd64 || arm64))

package natives

import "embed"

// files is empty: MyGo ships no libghostty-vt build for this platform.
var files embed.FS

const embedDir = "."
