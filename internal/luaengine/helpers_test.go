package luaengine_test

import (
	"testing"

	"github.com/digiogithub/pando/internal/luaengine"
)

func TestHelpers_RoundTrip(t *testing.T) {
	L := luaengine.NewLuaState()
	defer luaengine.CloseLuaState(L)

	tests := []struct {
		name  string
		value interface{}
	}{
		{"nil", nil},
		{"bool true", true},
		{"bool false", false},
		{"string", "hello world"},
		// int round-trips as int64 because Lua numbers are float64 and
		// LuaToGo converts whole-number floats to int64.
		{"int", int64(42)},
		{"int64", int64(1000)},
		{"float64", float64(3.14)},
		{"nested map", map[string]interface{}{
			"key":   "value",
			"count": int64(5),
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lv := luaengine.GoToLua(L, tt.value)
			got := luaengine.LuaToGo(lv)

			// For nil, both sides should be nil.
			if tt.value == nil {
				if got != nil {
					t.Errorf("RoundTrip(nil) = %v, want nil", got)
				}
				return
			}

			// For maps, compare by key presence.
			if orig, ok := tt.value.(map[string]interface{}); ok {
				gotMap, ok := got.(map[string]interface{})
				if !ok {
					t.Fatalf("expected map[string]interface{}, got %T", got)
				}
				for k, v := range orig {
					if gv, exists := gotMap[k]; !exists {
						t.Errorf("key %q missing from result", k)
					} else if gv != v {
						t.Errorf("key %q: got %v (%T), want %v (%T)", k, gv, gv, v, v)
					}
				}
				return
			}

			if got != tt.value {
				t.Errorf("RoundTrip(%v) = %v (%T), want %v (%T)", tt.value, got, got, tt.value, tt.value)
			}
		})
	}
}

func TestHelpers_MapToLuaTable(t *testing.T) {
	L := luaengine.NewLuaState()
	defer luaengine.CloseLuaState(L)

	m := map[string]interface{}{
		"a": "alpha",
		"b": int64(2),
		"c": true,
	}

	table := luaengine.MapToLuaTable(L, m)
	if table == nil {
		t.Fatal("MapToLuaTable returned nil")
	}

	result := luaengine.LuaTableToMap(table)
	if len(result) != len(m) {
		t.Errorf("expected %d keys, got %d", len(m), len(result))
	}
	for k, v := range m {
		if result[k] != v {
			t.Errorf("key %q: got %v, want %v", k, result[k], v)
		}
	}
}
