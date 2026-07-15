package settings

import "testing"

// TestFieldGeometryVariableHeights guards the click hit-testing and auto-scroll
// math against fields of mixed height: a field with a Hint renders taller than
// one without. A naive "every field is 3 lines" assumption used to mis-map
// clicks in the Agents/Models section (whose Max Tokens fields carry a hint).
func TestFieldGeometryVariableHeights(t *testing.T) {
	s := &Section{
		Fields: []Field{
			{Label: "Alpha", Type: FieldText, Value: "1"},            // no hint -> 3 lines
			{Label: "Beta", Type: FieldText, Value: "2", Hint: "hi"}, // hint -> 4 lines
			{Label: "Gamma", Type: FieldText, Value: "3"},            // no hint -> 3 lines
		},
	}
	const width = 60

	heights := s.FieldHeights(width)
	if len(heights) != 3 {
		t.Fatalf("expected 3 heights, got %d", len(heights))
	}
	if heights[0] != 3 || heights[2] != 3 {
		t.Errorf("fields without hint should be 3 lines, got %v", heights)
	}
	if heights[1] != 4 {
		t.Errorf("field with hint should be 4 lines, got %d", heights[1])
	}

	// Line offsets accumulate the real heights: 0, 3, 7.
	wantOffsets := []int{0, 3, 7}
	for idx, want := range wantOffsets {
		if got := s.FieldLineOffset(width, idx); got != want {
			t.Errorf("FieldLineOffset(%d) = %d, want %d", idx, got, want)
		}
	}

	// Every content line maps back to the field that occupies it.
	cases := map[int]int{
		0: 0, 2: 0, // Alpha rows 0..2
		3: 1, 6: 1, // Beta rows 3..6
		7: 2, 9: 2, // Gamma rows 7..9
		10: -1, // past the end
		-1: -1, // negative
	}
	for line, want := range cases {
		if got := s.FieldAtLine(width, line); got != want {
			t.Errorf("FieldAtLine(%d) = %d, want %d", line, got, want)
		}
	}
}
