package mcpauth

import (
	"reflect"
	"testing"
)

func TestUnionScopes_UnionDedupOrderStable(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want []string
	}{
		{name: "both empty", a: nil, b: nil, want: nil},
		{name: "a empty", a: nil, b: []string{"x", "y"}, want: []string{"x", "y"}},
		{name: "b empty", a: []string{"x", "y"}, b: nil, want: []string{"x", "y"}},
		{name: "no overlap preserves a-then-b order", a: []string{"a", "b"}, b: []string{"c", "d"}, want: []string{"a", "b", "c", "d"}},
		{name: "overlap dedups keeping first occurrence position", a: []string{"a", "b"}, b: []string{"b", "c"}, want: []string{"a", "b", "c"}},
		{name: "full overlap", a: []string{"a", "b"}, b: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "duplicates within a single input collapse", a: []string{"a", "a", "b"}, b: []string{"b", "c", "c"}, want: []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unionScopes(tt.a, tt.b)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("unionScopes(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestManager_StepUpScopes_NoPriorHistory(t *testing.T) {
	m := newTestManager(t)
	got := m.StepUpScopes("srv", []string{"read", "write"})
	want := []string{"read", "write"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StepUpScopes = %v, want %v", got, want)
	}
}

func TestManager_StepUpScopes_AccumulatesWithPriorHistory(t *testing.T) {
	m := newTestManager(t)
	if err := m.RecordGrantedScopes("srv", []string{"read"}); err != nil {
		t.Fatalf("RecordGrantedScopes: %v", err)
	}
	got := m.StepUpScopes("srv", []string{"write"})
	want := []string{"read", "write"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StepUpScopes = %v, want %v (accumulated, not replaced)", got, want)
	}
}

func TestManager_StepUpScopes_NoNewScopesIsNoOp(t *testing.T) {
	m := newTestManager(t)
	if err := m.RecordGrantedScopes("srv", []string{"read", "write"}); err != nil {
		t.Fatalf("RecordGrantedScopes: %v", err)
	}
	got := m.StepUpScopes("srv", []string{"read"})
	want := []string{"read", "write"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StepUpScopes = %v, want %v", got, want)
	}
}

func TestManager_RecordGrantedScopes_PersistsAcrossManagerInstances(t *testing.T) {
	m1 := newTestManager(t)
	if err := m1.RecordGrantedScopes("srv", []string{"read"}); err != nil {
		t.Fatalf("RecordGrantedScopes: %v", err)
	}
	// A fresh Manager over the same store must see the persisted scopes.
	m2 := NewManager(m1.store)
	got := m2.StepUpScopes("srv", []string{"write"})
	want := []string{"read", "write"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StepUpScopes (fresh manager) = %v, want %v", got, want)
	}
}

func TestErrInsufficientScope_Error(t *testing.T) {
	err := &ErrInsufficientScope{ServerName: "myserver", Scopes: []string{"read", "write"}}
	msg := err.Error()
	if want := `MCP server "myserver" needs additional permissions (scope: read write). Run: pando mcp login myserver`; msg != want {
		t.Errorf("Error() = %q, want %q", msg, want)
	}
}

func TestIsInsufficientScope(t *testing.T) {
	err := &ErrInsufficientScope{ServerName: "srv", Scopes: []string{"admin"}}
	got, ok := IsInsufficientScope(err)
	if !ok || got != err {
		t.Fatalf("IsInsufficientScope(err) = (%v, %v), want (%v, true)", got, ok, err)
	}

	if _, ok := IsInsufficientScope(nil); ok {
		t.Errorf("IsInsufficientScope(nil) = true, want false")
	}
}

func TestManager_HandleInsufficientScope_ReturnsErrInsufficientScope(t *testing.T) {
	m := newTestManager(t)
	err := m.HandleInsufficientScope("srv", []string{"admin"})
	scopeErr, ok := IsInsufficientScope(err)
	if !ok {
		t.Fatalf("HandleInsufficientScope did not return an ErrInsufficientScope: %v", err)
	}
	if scopeErr.ServerName != "srv" {
		t.Errorf("ServerName = %q, want srv", scopeErr.ServerName)
	}
	if !reflect.DeepEqual(scopeErr.Scopes, []string{"admin"}) {
		t.Errorf("Scopes = %v, want [admin]", scopeErr.Scopes)
	}
}

func TestManager_HandleInsufficientScope_CapsAttempts(t *testing.T) {
	m := newTestManager(t)

	// maxStepUpAttempts successful attempts must each still return
	// ErrInsufficientScope...
	for i := 0; i < maxStepUpAttempts; i++ {
		err := m.HandleInsufficientScope("srv", []string{"admin"})
		if _, ok := IsInsufficientScope(err); !ok {
			t.Fatalf("attempt %d: expected ErrInsufficientScope, got %v", i+1, err)
		}
	}

	// ...but the next attempt for the exact same accumulated scope set must
	// give up with a terminal (non-ErrInsufficientScope) error instead of
	// prompting for re-authorization yet again.
	err := m.HandleInsufficientScope("srv", []string{"admin"})
	if _, ok := IsInsufficientScope(err); ok {
		t.Fatalf("expected a terminal error after %d attempts, got ErrInsufficientScope: %v", maxStepUpAttempts, err)
	}
	if err == nil {
		t.Fatal("expected a non-nil terminal error")
	}
}

func TestManager_HandleInsufficientScope_DifferentScopeSetsHaveIndependentCaps(t *testing.T) {
	m := newTestManager(t)

	for i := 0; i < maxStepUpAttempts; i++ {
		_ = m.HandleInsufficientScope("srv", []string{"admin"})
	}
	// A different challenged scope produces a different accumulated set
	// (since RecordGrantedScopes was never called to persist "admin" as
	// granted), so it must get its own attempt budget rather than
	// inheriting "admin"'s exhausted one.
	err := m.HandleInsufficientScope("srv", []string{"other"})
	if _, ok := IsInsufficientScope(err); !ok {
		t.Fatalf("expected ErrInsufficientScope for an independent scope set, got %v", err)
	}
}
