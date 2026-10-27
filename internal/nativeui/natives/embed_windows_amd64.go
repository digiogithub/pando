package natives

import "embed"

// files holds libghostty-vt for windows/amd64 when the build fetched it
// (make natives-windows-amd64); otherwise only the .keep placeholder.
//
//go:embed all:windows-amd64
var files embed.FS

const embedDir = "windows-amd64"
