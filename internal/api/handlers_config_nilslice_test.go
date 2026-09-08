package api

import (
	"encoding/json"
	"testing"
)

// A config written before the desktop app lists existed leaves the slices nil.
// The response must still carry arrays: WebUI clients call .join() on them.
func TestToolsConfigResponseNeverEmitsNullAppLists(t *testing.T) {
	resp := ToolsConfigResponse{
		DesktopAllowedApps: nonNilStrings(nil),
		DesktopDeniedApps:  nonNilStrings(nil),
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"desktopAllowedApps", "desktopDeniedApps"} {
		v, ok := decoded[key]
		if !ok {
			t.Fatalf("%s missing from response", key)
		}
		if _, isSlice := v.([]any); !isSlice {
			t.Fatalf("%s = %v (%T), want an array", key, v, v)
		}
	}
}

func TestNonNilStringsKeepsValues(t *testing.T) {
	in := []string{"Firefox", "VSCode"}
	out := nonNilStrings(in)
	if len(out) != 2 || out[0] != "Firefox" || out[1] != "VSCode" {
		t.Fatalf("nonNilStrings(%v) = %v", in, out)
	}
}
