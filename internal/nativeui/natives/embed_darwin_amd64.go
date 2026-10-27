package natives

import "embed"

// files holds libghostty-vt for darwin/amd64 when the build fetched it
// (make natives-darwin-amd64); otherwise only the .keep placeholder.
//
//go:embed all:darwin-amd64
var files embed.FS

const embedDir = "darwin-amd64"
