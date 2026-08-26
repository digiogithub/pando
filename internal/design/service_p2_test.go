package design

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const p2Entry = `<!doctype html>
<html>
<head><meta charset="utf-8"><link rel="stylesheet" href="styles.css"></head>
<body>
  <h1 id="hero">Hello</h1>
  <img src="pixel.png" alt="p">
  <script src="app.js"></script>
</body>
</html>
`

func newPatchableArtifact(t *testing.T) (*Service, Artifact) {
	t.Helper()
	svc, _ := newTestService(t)
	artifact, err := svc.Create(context.Background(), CreateParams{
		Title: "Landing",
		Kind:  KindWeb,
		Files: map[string]string{
			"index.html": p2Entry,
			"styles.css": "body { margin: 0 }\n",
			"app.js":     "console.log('hi');\n",
			"pixel.png":  "\x89PNG\r\n\x1a\n",
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return svc, artifact
}

// A node id from the index is the whole point of the selection protocol: it has
// to resolve back to a source edit without the caller knowing any selector.
func TestPreparePatchResolvesNodeID(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)

	if err := svc.Store().ReplaceNodes(ctx, artifact.ID, artifact.CurrentVersion, []Node{{
		ArtifactID: artifact.ID,
		Version:    artifact.CurrentVersion,
		NodeID:     "n7",
		Selector:   "#hero",
		Role:       "heading",
		Text:       "Hello",
	}}); err != nil {
		t.Fatalf("index nodes: %v", err)
	}

	plan, version, err := svc.Patch(ctx, artifact.ID,
		[]PatchOp{{NodeID: "n7", Op: OpSetText, Value: "Bienvenido"}}, "rename hero", true)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if version != artifact.CurrentVersion+1 {
		t.Fatalf("commit should bump the version, got %d", version)
	}
	if len(plan.Files) != 1 || plan.Files[0].RelPath != "index.html" {
		t.Fatalf("patch targeted the wrong files: %+v", plan.Files)
	}

	absDir, _ := svc.AbsDir(artifact)
	out, err := os.ReadFile(filepath.Join(absDir, "index.html"))
	if err != nil {
		t.Fatalf("read entry: %v", err)
	}
	if !strings.Contains(string(out), `<h1 id="hero">Bienvenido</h1>`) {
		t.Fatalf("entry not patched:\n%s", out)
	}
}

func TestPreparePatchRejectsUnindexedNode(t *testing.T) {
	svc, artifact := newPatchableArtifact(t)
	_, err := svc.PreparePatch(context.Background(), artifact.ID,
		[]PatchOp{{NodeID: "ghost", Op: OpSetText, Value: "x"}})
	if err == nil || !strings.Contains(err.Error(), "design_render") {
		t.Fatalf("expected guidance to render first, got %v", err)
	}
}

func TestPreparePatchWritesNothingUntilApplied(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	absDir, _ := svc.AbsDir(artifact)
	entry := filepath.Join(absDir, "index.html")
	before, _ := os.ReadFile(entry)

	plan, err := svc.PreparePatch(ctx, artifact.ID, []PatchOp{{Selector: "#hero", Op: OpSetText, Value: "x"}})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	after, _ := os.ReadFile(entry)
	if string(before) != string(after) {
		t.Fatal("PreparePatch must not touch the working tree; the permission prompt happens between prepare and apply")
	}
	if plan.Empty() {
		t.Fatal("the plan should report a pending change")
	}
}

func TestPatchRejectsFileOutsideTheArtifact(t *testing.T) {
	svc, artifact := newPatchableArtifact(t)
	_, err := svc.PreparePatch(context.Background(), artifact.ID, []PatchOp{
		{Selector: "#hero", Op: OpSetText, Value: "x", File: "../../etc/passwd"},
	})
	if err == nil || !strings.Contains(err.Error(), "escapes the artifact directory") {
		t.Fatalf("expected an escape error, got %v", err)
	}
}

func TestExportHTMLInlinesLocalAssets(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)

	result, err := svc.Export(ctx, artifact.ID, ExportOptions{Format: ExportHTML})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	out := string(data)
	if strings.Contains(out, `href="styles.css"`) || !strings.Contains(out, "<style>") {
		t.Fatalf("stylesheet was not inlined:\n%s", out)
	}
	if strings.Contains(out, `src="app.js"`) || !strings.Contains(out, "console.log('hi');") {
		t.Fatalf("script was not inlined:\n%s", out)
	}
	if !strings.Contains(out, "src=\"data:image/png;base64,") {
		t.Fatalf("image was not embedded:\n%s", out)
	}
}

func TestExportHTMLLeavesRemoteReferencesAlone(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t)
	artifact, err := svc.Create(ctx, CreateParams{
		Title: "Remote",
		Files: map[string]string{
			"index.html": `<html><head><link rel="stylesheet" href="https://cdn.example/x.css"></head><body></body></html>`,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	result, err := svc.Export(ctx, artifact.ID, ExportOptions{Format: ExportHTML})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	data, _ := os.ReadFile(result.Path)
	if !strings.Contains(string(data), `href="https://cdn.example/x.css"`) {
		t.Fatal("a remote stylesheet must be left as-is: rewriting it would change what the design depends on")
	}
	if result.Note == "" {
		t.Fatal("the export should report what it could not inline")
	}
}

func TestDesignSystemCSSIsStable(t *testing.T) {
	svc, _ := newTestService(t)
	ds := DefaultDesignSystem()
	if _, _, err := svc.SaveSystem(ds); err != nil {
		t.Fatalf("save: %v", err)
	}
	cssPath := filepath.Join(svc.Layout().SystemPath(), SystemStylesheet)
	first, err := os.ReadFile(cssPath)
	if err != nil {
		t.Fatalf("read css: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, _, err := svc.SaveSystem(ds); err != nil {
			t.Fatalf("save: %v", err)
		}
		again, _ := os.ReadFile(cssPath)
		if string(again) != string(first) {
			t.Fatal("generated stylesheet must be byte-stable so token diffs stay readable")
		}
	}
	if !strings.Contains(string(first), "--color-accent: #2f6feb;") {
		t.Fatalf("tokens not emitted as custom properties:\n%s", first)
	}
}

func TestSetSystemTokensMergesAndRemoves(t *testing.T) {
	svc, _ := newTestService(t)
	if _, _, err := svc.SaveSystem(DefaultDesignSystem()); err != nil {
		t.Fatalf("save: %v", err)
	}
	updated, err := svc.SetSystemTokens("brand", map[string]map[string]string{
		"color": {"accent": "#ff0055", "muted": ""},
	})
	if err != nil {
		t.Fatalf("set tokens: %v", err)
	}
	if updated.Name != "brand" {
		t.Fatalf("name not updated: %q", updated.Name)
	}
	if updated.Tokens["color"]["accent"] != "#ff0055" {
		t.Fatal("token not merged")
	}
	if _, ok := updated.Tokens["color"]["muted"]; ok {
		t.Fatal("an empty value should remove the token")
	}
	if updated.Tokens["space"]["md"] != "16px" {
		t.Fatal("untouched groups must survive a merge")
	}
}

func TestPresentationReportsEntryAndSelection(t *testing.T) {
	ctx := context.Background()
	svc, artifact := newPatchableArtifact(t)
	if err := svc.Store().ReplaceNodes(ctx, artifact.ID, artifact.CurrentVersion, []Node{{
		ArtifactID: artifact.ID, Version: artifact.CurrentVersion, NodeID: "n1", Selector: "#hero",
	}}); err != nil {
		t.Fatalf("index: %v", err)
	}

	p, err := svc.Presentation(ctx, artifact.ID, 0, "n1")
	if err != nil {
		t.Fatalf("presentation: %v", err)
	}
	if !strings.HasPrefix(p.URL, "file://") || !strings.HasSuffix(p.URL, "index.html") {
		t.Fatalf("unexpected URL %q", p.URL)
	}
	if p.Selection != "design://n1" {
		t.Fatalf("selection protocol wrong: %q", p.Selection)
	}
	if id, ok := ParseSelectionURI(p.Selection); !ok || id != "n1" {
		t.Fatalf("selection round-trip failed: %q %v", id, ok)
	}

	if _, err := svc.Presentation(ctx, artifact.ID, 0, "ghost"); err == nil {
		t.Fatal("an unindexed node must not be presentable as a selection")
	}
}

func TestWriteWorkspaceFileRefusesEscape(t *testing.T) {
	svc, project := newTestService(t)
	rel, err := svc.WriteWorkspaceFile("designer/landing/assets/bg.png", []byte("png"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if _, err := svc.WriteWorkspaceFile("../escape.png", []byte("png")); err == nil {
		t.Fatal("writing outside the working directory must be refused")
	}
}
