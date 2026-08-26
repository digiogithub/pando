package design

import (
	"strings"
	"testing"
)

const patchFixture = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Fixture</title>
  <link rel="stylesheet" href="styles.css">
</head>
<body>
  <header class="site-header">
    <h1 id="hero" class="title big" data-role="hero">Original   heading</h1>
  </header>
  <main>
    <section class="card" style="color: red; padding: 4px">One</section>
    <section class="card">Two</section>
    <section class="card">Three</section>
    <img src="logo.png" alt="Logo">
  </main>
</body>
</html>
`

func patch(t *testing.T, src string, ops ...PatchOp) string {
	t.Helper()
	out, _, err := ApplyPatch([]byte(src), ops)
	if err != nil {
		t.Fatalf("ApplyPatch: %v", err)
	}
	return string(out)
}

// The patch engine must splice bytes rather than reserialise the document:
// artifact files are committed and hand-edited, so everything outside the
// targeted element has to survive byte for byte.
func TestPatchPreservesEverythingOutsideTheTarget(t *testing.T) {
	out := patch(t, patchFixture, PatchOp{Selector: "#hero", Op: OpSetText, Value: "New heading"})

	if !strings.Contains(out, ">New heading</h1>") {
		t.Fatalf("heading was not replaced:\n%s", out)
	}
	if strings.Contains(out, "Original   heading") {
		t.Fatal("old heading text survived")
	}
	// Untouched regions, including irregular whitespace and the doctype.
	for _, want := range []string{
		"<!doctype html>\n<html lang=\"en\">",
		`<link rel="stylesheet" href="styles.css">`,
		`<section class="card" style="color: red; padding: 4px">One</section>`,
		"  </main>\n</body>\n</html>\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("patch disturbed untouched region %q:\n%s", want, out)
		}
	}
}

func TestPatchNthOfTypeSelector(t *testing.T) {
	out := patch(t, patchFixture, PatchOp{
		Selector: "main > section:nth-of-type(3)", Op: OpSetText, Value: "Third",
	})
	if !strings.Contains(out, `<section class="card">Third</section>`) {
		t.Fatalf("third section not patched:\n%s", out)
	}
	if !strings.Contains(out, `<section class="card">Two</section>`) {
		t.Fatal("second section should not have changed")
	}
}

func TestPatchRefusesAmbiguousSelector(t *testing.T) {
	_, _, err := ApplyPatch([]byte(patchFixture), []PatchOp{
		{Selector: ".card", Op: OpAddClass, Class: "wide"},
	})
	if err == nil {
		t.Fatal("expected an error for a selector matching three elements")
	}
	if !strings.Contains(err.Error(), "matched 3 elements") {
		t.Fatalf("error should say how many matched, got: %v", err)
	}

	out := patch(t, patchFixture, PatchOp{Selector: ".card", Op: OpAddClass, Class: "wide", All: true})
	if n := strings.Count(out, "card wide"); n != 3 {
		t.Fatalf(`with "all" every match should be patched, got %d:\n%s`, n, out)
	}
}

func TestPatchAttributeOperations(t *testing.T) {
	out := patch(t, patchFixture,
		PatchOp{Selector: "img", Op: OpSetAttr, Attr: "alt", Value: `A "logo" & mark`},
		PatchOp{Selector: "img", Op: OpSetAttr, Attr: "loading", Value: "lazy"},
		PatchOp{Selector: "#hero", Op: OpRemoveAttr, Attr: "data-role"},
	)
	if !strings.Contains(out, `alt="A &quot;logo&quot; &amp; mark"`) {
		t.Fatalf("attribute value was not escaped:\n%s", out)
	}
	if !strings.Contains(out, `<img src="logo.png" alt="A &quot;logo&quot; &amp; mark" loading="lazy">`) {
		t.Fatalf("new attribute was not appended in place:\n%s", out)
	}
	if strings.Contains(out, "data-role") {
		t.Fatal("attribute was not removed")
	}
	if !strings.Contains(out, `<h1 id="hero" class="title big">`) {
		t.Fatalf("removing an attribute left stray whitespace:\n%s", out)
	}
}

func TestPatchStyleMergePreservesOrderAndSortsAdditions(t *testing.T) {
	out := patch(t, patchFixture, PatchOp{
		Selector: "section:nth-of-type(1)",
		Op:       OpSetStyle,
		Style:    map[string]string{"padding": "12px", "background": "#fff", "color": ""},
	})
	// color was dropped, padding kept its position and was updated, background
	// was appended.
	if !strings.Contains(out, `style="padding: 12px; background: #fff"`) {
		t.Fatalf("style merge is wrong:\n%s", out)
	}
}

func TestMergeStyleIsDeterministic(t *testing.T) {
	updates := map[string]string{"z-index": "2", "color": "red", "margin": "0", "border": "none"}
	first := mergeStyle("", updates)
	for i := 0; i < 20; i++ {
		if got := mergeStyle("", updates); got != first {
			t.Fatalf("mergeStyle is not deterministic: %q vs %q", first, got)
		}
	}
	if first != "border: none; color: red; margin: 0; z-index: 2" {
		t.Fatalf("additions should be sorted by property, got %q", first)
	}
}

func TestPatchClassOperations(t *testing.T) {
	out := patch(t, patchFixture,
		PatchOp{Selector: "#hero", Op: OpRemoveClass, Class: "big"},
		PatchOp{Selector: "header", Op: OpAddClass, Class: "sticky"},
	)
	if !strings.Contains(out, `class="title"`) {
		t.Fatalf("class not removed:\n%s", out)
	}
	if !strings.Contains(out, `class="site-header sticky"`) {
		t.Fatalf("class not appended:\n%s", out)
	}
}

func TestPatchInsertAndRemove(t *testing.T) {
	out := patch(t, patchFixture,
		PatchOp{Selector: "header", Op: OpInsertHTML, Position: PositionAppend, HTML: "\n    <p>tag</p>"},
		PatchOp{Selector: "img", Op: OpRemove},
	)
	if !strings.Contains(out, "<p>tag</p></header>") {
		t.Fatalf("markup not appended inside the header:\n%s", out)
	}
	if strings.Contains(out, "logo.png") {
		t.Fatal("image was not removed")
	}
}

func TestPatchVoidElementText(t *testing.T) {
	_, _, err := ApplyPatch([]byte(patchFixture), []PatchOp{
		{Selector: "img", Op: OpSetText, Value: "nope"},
	})
	if err == nil || !strings.Contains(err.Error(), "void element") {
		t.Fatalf("expected a void-element error, got %v", err)
	}
}

func TestPatchRejectsOverlappingOperations(t *testing.T) {
	_, _, err := ApplyPatch([]byte(patchFixture), []PatchOp{
		{Selector: "header", Op: OpRemove},
		{Selector: "#hero", Op: OpSetText, Value: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "conflicting patch operations") {
		t.Fatalf("expected a conflict error, got %v", err)
	}
}

func TestPatchTextIsEscaped(t *testing.T) {
	out := patch(t, patchFixture, PatchOp{
		Selector: "#hero", Op: OpSetText, Value: `<script>alert("x")</script> & more`,
	})
	if strings.Contains(out, "<script>alert") {
		t.Fatalf("set_text must escape markup:\n%s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;alert(\"x\")&lt;/script&gt; &amp; more") {
		t.Fatalf("escaped text is wrong:\n%s", out)
	}
}

func TestPatchUnknownSelectorIsActionable(t *testing.T) {
	_, _, err := ApplyPatch([]byte(patchFixture), []PatchOp{
		{Selector: "#nope", Op: OpSetText, Value: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "matched no element") {
		t.Fatalf("expected a no-match error, got %v", err)
	}
	if !strings.Contains(err.Error(), "created by a script") {
		t.Fatalf("the no-match error should explain the script case, got: %v", err)
	}
}

// Unclosed <li> is the common authoring shape the browser repairs; the source
// tree has to match the DOM the renderer indexed or node ids would resolve to
// the wrong element.
func TestPatchHandlesImplicitlyClosedListItems(t *testing.T) {
	src := "<ul>\n  <li>one\n  <li>two\n  <li>three\n</ul>\n"
	out := patch(t, src, PatchOp{Selector: "li:nth-of-type(2)", Op: OpSetText, Value: "TWO"})
	if !strings.Contains(out, "<li>TWO") {
		t.Fatalf("second list item not patched:\n%s", out)
	}
	if !strings.Contains(out, "<li>one") || !strings.Contains(out, "<li>three") {
		t.Fatalf("sibling list items were disturbed:\n%s", out)
	}
}

func TestSelectorParsingRejectsUnsupportedSyntax(t *testing.T) {
	for _, s := range []string{"a, b", "div:hover", "div[", ""} {
		if _, err := parseSelector(s); err == nil {
			t.Fatalf("selector %q should have been rejected", s)
		}
	}
	for _, s := range []string{"#id", "div.card", "main > section:nth-of-type(2)", `[data-slide="1"]`, "body div span"} {
		if _, err := parseSelector(s); err != nil {
			t.Fatalf("selector %q should parse: %v", s, err)
		}
	}
}

func TestParseStartTagRecordsAttributeRanges(t *testing.T) {
	src := `<div id="a" hidden class='b c'>`
	tag, attrs, selfClosing := parseStartTag([]byte(src), 0)
	if tag != "div" || selfClosing {
		t.Fatalf("tag=%q selfClosing=%v", tag, selfClosing)
	}
	if len(attrs) != 3 {
		t.Fatalf("expected 3 attributes, got %d", len(attrs))
	}
	if got := src[attrs[0].valStart:attrs[0].valEnd]; got != "a" {
		t.Fatalf("id value range wrong: %q", got)
	}
	if attrs[1].valStart != -1 {
		t.Fatal("a bare attribute must have no value range")
	}
	if attrs[2].quote != '\'' {
		t.Fatalf("quote character not recorded: %q", attrs[2].quote)
	}
}
