package windows

import (
	"strconv"
	"strings"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

// EncodeRuntimeID renders a UIA RuntimeId ([]int32, as returned by
// IUIAutomationElement::GetRuntimeId) as a stable, comparable string, used
// both as the durable element identity stashed in core.Element.Native.Data
// (see element.go's nativeRuntimeIDKey) and as the backend's handle-table
// key. RuntimeId segments are UIA-internal integers (frequently including a
// negative "synthetic" leading segment); they are joined by '.', each
// formatted as a signed decimal so the encoding round-trips exactly.
func EncodeRuntimeID(id []int32) string {
	if len(id) == 0 {
		return ""
	}
	parts := make([]string, len(id))
	for i, v := range id {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return strings.Join(parts, ".")
}

// DecodeRuntimeID parses a string produced by EncodeRuntimeID back into a
// RuntimeId. It returns an INVALID_ARGS core.DesktopError on malformed
// input.
func DecodeRuntimeID(s string) ([]int32, error) {
	if strings.TrimSpace(s) == "" {
		return nil, core.NewInvalidArgsError("empty UIA runtime id")
	}
	parts := strings.Split(s, ".")
	out := make([]int32, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseInt(p, 10, 32)
		if err != nil {
			return nil, core.NewInvalidArgsError("malformed UIA runtime id segment " + strconv.Quote(p) + " in " + strconv.Quote(s))
		}
		out = append(out, int32(n))
	}
	return out, nil
}
