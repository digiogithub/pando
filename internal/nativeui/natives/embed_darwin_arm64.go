package natives

import "embed"

// files holds libghostty-vt for darwin/arm64 when the build fetched it
// (make natives-darwin-arm64); otherwise only the .keep placeholder.
//
//go:embed all:darwin-arm64
var files embed.FS

const embedDir = "darwin-arm64"
