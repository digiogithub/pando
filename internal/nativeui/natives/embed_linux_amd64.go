package natives

import "embed"

// files holds libghostty-vt for linux/amd64 when the build fetched it
// (make natives-linux-amd64); otherwise only the .keep placeholder.
//
//go:embed all:linux-amd64
var files embed.FS

const embedDir = "linux-amd64"
