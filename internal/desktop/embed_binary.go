package desktop

import _ "embed"

// DesktopBinary is the embedded pre-compiled pando-desktop binary for non-macOS
// builds. Populated by running `make desktop-embed`.
//
//go:embed bin/pando-desktop
var DesktopBinary []byte
