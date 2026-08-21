package extension

import (
	"testing"
)

// stub is a minimal extension used across the tests in this package.
type stub struct {
	info Info
}

func (s stub) ExtensionInfo() Info { return s.info }

func newStub(id ID, opts ...func(*Info)) stub {
	info := Info{ID: id, Name: string(id), Version: "1.0.0"}
	for _, opt := range opts {
		opt(&info)
	}
	if info.New == nil {
		info.New = func() Extension { return stub{info: info} }
	}
	return stub{info: info}
}

func TestIDValid(t *testing.T) {
	tests := []struct {
		id   ID
		want bool
	}{
		{"tools.acme.jira", true},
		{"memory", true},
		{"a-b_c.d0", true},
		{"", false},
		{"tools..jira", false},
		{".tools", false},
		{"tools.", false},
		{"Tools.Jira", false},
		{"tools jira", false},
	}
	for _, tt := range tests {
		if got := tt.id.Valid(); got != tt.want {
			t.Errorf("ID(%q).Valid() = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestIDNamespace(t *testing.T) {
	if got := ID("tools.acme.jira").Namespace(); got != "tools.acme" {
		t.Errorf("Namespace() = %q, want %q", got, "tools.acme")
	}
	if got := ID("memory").Namespace(); got != "" {
		t.Errorf("Namespace() = %q, want empty", got)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("tools.acme.jira"))

	info, ok := r.Get("tools.acme.jira")
	if !ok {
		t.Fatal("Get returned not-found for a registered extension")
	}
	if info.License != LicenseMIT {
		t.Errorf("License = %q, want default %q", info.License, LicenseMIT)
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("tools.acme.jira"))

	defer func() {
		if recover() == nil {
			t.Fatal("duplicate registration did not panic")
		}
	}()
	r.Register(newStub("tools.acme.jira"))
}

func TestRegistryRejectsInvalidID(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("invalid ID did not panic")
		}
	}()
	r.Register(newStub("Tools.Jira"))
}

func TestRegistryRejectsMissingFactory(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("missing New factory did not panic")
		}
	}()
	r.Register(stub{info: Info{ID: "tools.acme.jira"}})
}

func TestRegistryListSortedAndByNamespace(t *testing.T) {
	r := NewRegistry()
	r.Register(newStub("tools.b"))
	r.Register(newStub("tools.a"))
	r.Register(newStub("memory.sink.corp"))

	list := r.List()
	if len(list) != 3 {
		t.Fatalf("List returned %d entries, want 3", len(list))
	}
	if list[0].ID != "memory.sink.corp" || list[1].ID != "tools.a" || list[2].ID != "tools.b" {
		t.Errorf("List is not sorted by ID: %v, %v, %v", list[0].ID, list[1].ID, list[2].ID)
	}

	tools := r.ByNamespace("tools")
	if len(tools) != 2 {
		t.Fatalf("ByNamespace(tools) returned %d entries, want 2", len(tools))
	}

	// A namespace must not match an ID that merely shares a prefix.
	r.Register(newStub("toolsx.c"))
	if got := len(r.ByNamespace("tools")); got != 2 {
		t.Errorf("ByNamespace(tools) matched a prefix-sharing ID: got %d, want 2", got)
	}
}
