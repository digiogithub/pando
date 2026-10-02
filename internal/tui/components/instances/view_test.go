package instances

import (
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/instanceregistry"
)

func TestInstanceMetaIncludesWebPortAndParentMarker(t *testing.T) {
	entry := &instanceregistry.Entry{
		Mode:             instanceregistry.ModeWebUI,
		Path:             "/workspace/project-one",
		WebPort:          4123,
		ParentInstanceID: "parent-12345678",
	}

	meta := instanceMeta(entry)
	if !strings.Contains(meta, "web :4123") {
		t.Fatalf("meta %q missing web port", meta)
	}
	if !strings.Contains(meta, "child parent-1") {
		t.Fatalf("meta %q missing shortened parent id", meta)
	}
}

func TestViewShowsWebPortAndChildMarker(t *testing.T) {
	m := New()
	m.SetSize(100, 18)
	m.instances = []*instanceregistry.Entry{
		{
			Mode:             instanceregistry.ModeWebUI,
			Path:             "/workspace/project-one",
			PID:              1234,
			RPCPort:          4100,
			PubPort:          4101,
			WebPort:          4123,
			ParentInstanceID: "12345678-parent",
			StartedAt:        time.Now(),
		},
	}

	view := m.View()
	if !strings.Contains(view, "web :4123") {
		t.Fatalf("view %q missing web port", view)
	}
	if !strings.Contains(view, "child 12345678") {
		t.Fatalf("view %q missing child marker", view)
	}
}
