package linux

import "testing"

func stateWord(bits ...int) []uint32 {
	var words [2]uint32
	for _, b := range bits {
		words[b/32] |= 1 << uint(b%32)
	}
	return words[:]
}

func TestDecodeState(t *testing.T) {
	raw := stateWord(stateEnabled, stateSensitive, stateVisible, stateShowing, stateFocused,
		stateFocusable, stateChecked, stateSelected, stateExpanded, stateCheckable)
	ds := decodeState(raw)

	if !ds.Enabled || !ds.Sensitive {
		t.Fatalf("expected Enabled and Sensitive set, got %+v", ds)
	}
	if !ds.Visible || !ds.Showing {
		t.Fatalf("expected Visible and Showing set, got %+v", ds)
	}
	if !ds.Focused || !ds.Focusable {
		t.Fatalf("expected Focused and Focusable set, got %+v", ds)
	}
	if !ds.Checked || !ds.Selected || !ds.Expanded || !ds.Checkable {
		t.Fatalf("expected Checked/Selected/Expanded/Checkable set, got %+v", ds)
	}
	if ds.Pressed || ds.Busy || ds.Editable {
		t.Fatalf("expected Pressed/Busy/Editable unset, got %+v", ds)
	}
}

func TestDecodeStateSecondWord(t *testing.T) {
	// stateCheckable(41) lives in the second 32-bit word (bit 9 of word 1).
	raw := stateWord(stateCheckable)
	if len(raw) != 2 {
		t.Fatalf("expected a 2-word state slice")
	}
	if raw[0] != 0 {
		t.Fatalf("expected word 0 to be empty for a second-word-only bit, got %x", raw[0])
	}
	ds := decodeState(raw)
	if !ds.Checkable {
		t.Fatalf("expected Checkable to decode from the second word")
	}
}

func TestDecodeStateShortSlice(t *testing.T) {
	// A backend that only returns one word (or none) must not panic and
	// must treat unreachable bits as unset.
	ds := decodeState([]uint32{0})
	if ds.Checkable || ds.Enabled {
		t.Fatalf("expected all-false decode for a short/empty state slice, got %+v", ds)
	}
	ds = decodeState(nil)
	if ds.Enabled {
		t.Fatalf("expected all-false decode for a nil state slice")
	}
}

func TestElementEnabledCombinesBits(t *testing.T) {
	cases := []struct {
		enabled, sensitive, want bool
	}{
		{true, false, true},
		{false, true, true},
		{true, true, true},
		{false, false, false},
	}
	for _, c := range cases {
		ds := decodedState{Enabled: c.enabled, Sensitive: c.sensitive}
		if got := ds.elementEnabled(); got != c.want {
			t.Errorf("elementEnabled(enabled=%v,sensitive=%v) = %v, want %v", c.enabled, c.sensitive, got, c.want)
		}
	}
}

func TestElementVisibleRequiresBothBits(t *testing.T) {
	cases := []struct {
		visible, showing, want bool
	}{
		{true, true, true},
		{true, false, false},
		{false, true, false},
		{false, false, false},
	}
	for _, c := range cases {
		ds := decodedState{Visible: c.visible, Showing: c.showing}
		if got := ds.elementVisible(); got != c.want {
			t.Errorf("elementVisible(visible=%v,showing=%v) = %v, want %v", c.visible, c.showing, got, c.want)
		}
	}
}

func TestNativeExtrasCarriesUndeclaredFields(t *testing.T) {
	ds := decodedState{Checked: true, Selected: true, Expanded: true, Focusable: true}
	extras := ds.nativeExtras()
	for _, key := range []string{"checked", "selected", "expanded", "focusable"} {
		v, ok := extras[key].(bool)
		if !ok || !v {
			t.Errorf("expected extras[%q] to be true, got %v (ok=%v)", key, extras[key], ok)
		}
	}
}
