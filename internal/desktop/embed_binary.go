package desktop

import _ "embed"

// DesktopBinary is the embedded pre-compiled pando-desktop binary.
// Populated by running `make desktop-embed`.
//
//go:embed bin/pando-desktop
var DesktopBinary []byte
