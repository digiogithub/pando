package tools

import (
	"reflect"
	"testing"

	"github.com/digiogithub/pando/internal/rag/kb"
)

func TestKBLinkViewsOmitsEmpty(t *testing.T) {
	// A document with no links must produce no section at all: the tools rely on
	// the nil result to leave "links" out of the response entirely, so a knowledge
	// base that does not use the syntax reads exactly as it did before.
	if v := kbLinkViews(nil); v != nil {
		t.Errorf("kbLinkViews(nil) = %v, want nil", v)
	}
	if v := kbBacklinkViews(nil); v != nil {
		t.Errorf("kbBacklinkViews(nil) = %v, want nil", v)
	}
	if v := kbRelatedViews(nil); v != nil {
		t.Errorf("kbRelatedViews(nil) = %v, want nil", v)
	}
}

func TestKBLinkViewsCarryResolution(t *testing.T) {
	views := kbLinkViews([]kb.LinkTarget{
		{
			WikiLink: kb.WikiLink{Raw: "Code Graph", Slug: "code-graph", Label: "the graph"},
			FilePath: "pando/features/code_graph.md",
			Resolved: true,
		},
		{WikiLink: kb.WikiLink{Raw: "token ledger", Slug: "token-ledger"}},
	})

	want := []kbLinkView{
		{
			Target:   "Code Graph",
			Slug:     "code-graph",
			Label:    "the graph",
			FilePath: "pando/features/code_graph.md",
			Resolved: true,
		},
		{Target: "token ledger", Slug: "token-ledger"},
	}
	if !reflect.DeepEqual(views, want) {
		t.Errorf("kbLinkViews() = %+v, want %+v", views, want)
	}
}

func TestUnresolvedTargetsKeepsTheAuthorsSpelling(t *testing.T) {
	targets := []kb.LinkTarget{
		{WikiLink: kb.WikiLink{Raw: "beta", Slug: "beta"}, FilePath: "beta.md", Resolved: true},
		{WikiLink: kb.WikiLink{Raw: "Token Ledger", Slug: "token-ledger"}},
		{WikiLink: kb.WikiLink{Raw: "Graph View", Slug: "graph-view"}},
	}

	got := unresolvedTargets(targets)
	want := []string{"Token Ledger", "Graph View"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unresolvedTargets() = %v, want %v", got, want)
	}

	if got := unresolvedTargets(targets[:1]); got != nil {
		t.Errorf("unresolvedTargets(all resolved) = %v, want nil", got)
	}
}
