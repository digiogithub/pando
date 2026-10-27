package natives

import "embed"

// files holds libghostty-vt for linux/arm64 when the build fetched it
// (make natives-linux-arm64); otherwise only the .keep placeholder.
//
//go:embed all:linux-arm64
var files embed.FS

const embedDir = "linux-arm64"
