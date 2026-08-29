package windows

import (
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/uiauto/core"
)

func TestEncodeDecodeRuntimeIDRoundTrip(t *testing.T) {
	cases := [][]int32{
		{1},
		{42, 3, 7},
		{-1, 2, 3, 4},
		{0, 0, 0},
	}
	for _, id := range cases {
		enc := EncodeRuntimeID(id)
		if enc == "" {
			t.Fatalf("EncodeRuntimeID(%v) returned empty string", id)
		}
		dec, err := DecodeRuntimeID(enc)
		if err != nil {
			t.Fatalf("DecodeRuntimeID(%q) error: %v", enc, err)
		}
		if !reflect.DeepEqual(dec, id) {
			t.Fatalf("round trip mismatch: got %v, want %v", dec, id)
		}
	}
}

func TestEncodeRuntimeIDEmpty(t *testing.T) {
	if got := EncodeRuntimeID(nil); got != "" {
		t.Fatalf("EncodeRuntimeID(nil) = %q, want empty", got)
	}
}

func TestDecodeRuntimeIDInvalid(t *testing.T) {
	for _, s := range []string{"", "  ", "abc", "1.2.x", "1..2"} {
		_, err := DecodeRuntimeID(s)
		if err == nil {
			t.Fatalf("DecodeRuntimeID(%q) expected error, got nil", s)
		}
		de, ok := core.AsDesktopError(err)
		if !ok || de.Code != core.ErrInvalidArgs {
			t.Fatalf("DecodeRuntimeID(%q) error = %v, want INVALID_ARGS DesktopError", s, err)
		}
	}
}
