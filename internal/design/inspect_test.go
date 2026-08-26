package design

import (
	"strings"
	"testing"
)

func indexFixture() []Node {
	return []Node{
		{ArtifactID: "dsg_1", Version: 1, NodeID: "n1", Selector: "body", Role: "body", Box: Rect{Y: 0, H: 900}},
		{ArtifactID: "dsg_1", Version: 1, NodeID: "n2", ParentID: "n1", Selector: "#hero", Role: "section", Box: Rect{Y: 10, H: 400}},
		{ArtifactID: "dsg_1", Version: 1, NodeID: "n3", ParentID: "n2", Selector: "#hero > h1", Role: "heading",
			Text: "Ship design faster", Box: Rect{Y: 40, H: 60}, Styles: map[string]string{"color": "rgb(0,0,0)", "font-size": "48px"}},
		{ArtifactID: "dsg_1", Version: 1, NodeID: "n4", ParentID: "n2", Selector: "#hero-cta", Role: "button",
			Text: "Get started", Box: Rect{Y: 120, H: 44}, Styles: map[string]string{"color": "rgb(255,255,255)", "font-size": "16px"}},
		{ArtifactID: "dsg_1", Version: 1, NodeID: "n5", ParentID: "n1", Selector: "#footer", Role: "contentinfo",
			Slide: 1, Box: Rect{Y: 800, H: 100}},
	}
}

func TestInspectPagesAndReportsNextOffset(t *testing.T) {
	nodes := indexFixture()

	first := Inspect(nodes, InspectOptions{Slide: -1, Limit: 2})
	if first.Total != len(nodes) {
		t.Fatalf("total = %d, want %d", first.Total, len(nodes))
	}
	if len(first.Nodes) != 2 {
		t.Fatalf("page size = %d, want 2", len(first.Nodes))
	}
	if first.NextOffset != 2 {
		t.Fatalf("next offset = %d, want 2", first.NextOffset)
	}

	last := Inspect(nodes, InspectOptions{Slide: -1, Offset: 4, Limit: 2})
	if last.NextOffset != -1 {
		t.Fatalf("last page reported more pages (next=%d)", last.NextOffset)
	}

	beyond := Inspect(nodes, InspectOptions{Slide: -1, Offset: 99})
	if len(beyond.Nodes) != 0 || beyond.NextOffset != -1 {
		t.Fatalf("offset past the end returned %+v", beyond)
	}
}

func TestInspectFilters(t *testing.T) {
	nodes := indexFixture()

	bySelector := Inspect(nodes, InspectOptions{Slide: -1, Selector: "hero-cta"})
	if len(bySelector.Nodes) != 1 || bySelector.Nodes[0].NodeID != "n4" {
		t.Fatalf("selector filter returned %+v", bySelector.Nodes)
	}

	byRole := Inspect(nodes, InspectOptions{Slide: -1, Selector: "heading"})
	if len(byRole.Nodes) != 1 || byRole.Nodes[0].NodeID != "n3" {
		t.Fatalf("role filter returned %+v", byRole.Nodes)
	}

	byText := Inspect(nodes, InspectOptions{Slide: -1, Text: "get started"})
	if len(byText.Nodes) != 1 || byText.Nodes[0].NodeID != "n4" {
		t.Fatalf("text filter returned %+v", byText.Nodes)
	}

	bySlide := Inspect(nodes, InspectOptions{Slide: 1})
	if len(bySlide.Nodes) != 1 || bySlide.Nodes[0].NodeID != "n5" {
		t.Fatalf("slide filter returned %+v", bySlide.Nodes)
	}

	subtree := Inspect(nodes, InspectOptions{Slide: -1, NodeID: "n2"})
	got := map[string]bool{}
	for _, n := range subtree.Nodes {
		got[n.NodeID] = true
	}
	if len(got) != 3 || !got["n2"] || !got["n3"] || !got["n4"] {
		t.Fatalf("subtree filter returned %v", got)
	}

	shallow := Inspect(nodes, InspectOptions{Slide: -1, NodeID: "n2", Depth: 1})
	for _, n := range shallow.Nodes {
		if n.NodeID == "n3" || n.NodeID == "n4" {
			continue
		}
		if n.NodeID != "n2" {
			t.Fatalf("depth filter leaked %s", n.NodeID)
		}
	}
}

func TestInspectDropsStylesUnlessAsked(t *testing.T) {
	nodes := indexFixture()

	lean := Inspect(nodes, InspectOptions{Slide: -1, Selector: "#hero > h1"})
	if lean.Nodes[0].Styles != nil {
		t.Fatalf("styles carried without IncludeStyles: %+v", lean.Nodes[0].Styles)
	}

	full := Inspect(nodes, InspectOptions{Slide: -1, Selector: "#hero > h1", IncludeStyles: true})
	if len(full.Nodes[0].Styles) != 2 {
		t.Fatalf("styles = %+v, want 2 properties", full.Nodes[0].Styles)
	}

	narrowed := Inspect(nodes, InspectOptions{
		Slide: -1, Selector: "#hero > h1", IncludeStyles: true, StyleProps: []string{"font-size"},
	})
	if len(narrowed.Nodes[0].Styles) != 1 || narrowed.Nodes[0].Styles["font-size"] != "48px" {
		t.Fatalf("style subset = %+v", narrowed.Nodes[0].Styles)
	}
}

func TestInspectTruncatesText(t *testing.T) {
	nodes := indexFixture()
	result := Inspect(nodes, InspectOptions{Slide: -1, Selector: "#hero > h1", MaxTextLen: 4})
	if result.Nodes[0].Text != "Ship…" {
		t.Fatalf("text = %q, want %q", result.Nodes[0].Text, "Ship…")
	}
}

func TestInspectTextRendering(t *testing.T) {
	nodes := indexFixture()
	out := Inspect(nodes, InspectOptions{Slide: -1, Limit: 2}).Text()

	if !strings.HasPrefix(out, "nodes 1-2 of 5") {
		t.Fatalf("header missing: %q", out)
	}
	if !strings.Contains(out, "more nodes available: offset=2") {
		t.Fatalf("pagination hint missing: %q", out)
	}
	if strings.Contains(out, "{") {
		t.Fatalf("styles rendered without IncludeStyles: %q", out)
	}

	empty := Inspect(nodes, InspectOptions{Slide: -1, Selector: "nothing-matches"}).Text()
	if !strings.Contains(empty, "(no matching nodes)") {
		t.Fatalf("empty rendering = %q", empty)
	}
}
