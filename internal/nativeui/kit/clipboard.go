package kit

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
)

// CopyText puts s on the system clipboard and reports success.
func CopyText(s string) bool { return mygo.Clipboard.Write(transfer.TextData(s)) == nil }
