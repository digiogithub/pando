package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/luaengine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeVariantEvaluator freezes a fixed choice per section and records calls.
type fakeVariantEvaluator struct {
	choose map[string]string // section -> variant name to pick (default when absent)
	calls  []string
}

func (f *fakeVariantEvaluator) SelectVariant(_ context.Context, _, section string, candidates []string) (string, error) {
	f.calls = append(f.calls, section)
	if v, ok := f.choose[section]; ok {
		return VariantID(section, v), nil
	}
	return candidates[0], nil
}
func (f *fakeVariantEvaluator) GetActiveSkills(context.Context, string) ([]PromptEvaluatorSkill, error) {
	return nil, nil
}
func (f *fakeVariantEvaluator) ClassifyTask(string) string { return "general" }

func writeVariant(t *testing.T, wd, section, name, body string) string {
	t.Helper()
	dir := filepath.Join(wd, ".pando", "prompts", "variants", filepath.FromSlash(section))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, name+".md.tpl")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func variantBuilder(t *testing.T, wd string, ev PromptEvaluator, lua *luaengine.FilterManager) *PromptBuilder {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // keep the global variants dir out of the test
	data := &PromptData{
		AgentName:  "coder",
		WorkingDir: wd,
		Platform:   "linux",
		Date:       "9/29/2026",
		Provider:   "anthropic",
	}
	b := NewPromptBuilder("coder", "anthropic", data, lua)
	b.SetEvaluator(ev)
	return b
}

func sessionCtx() context.Context {
	return context.WithValue(context.Background(), SessionIDKey, "sess-1")
}

func TestDiscoverVariants(t *testing.T) {
	wd := t.TempDir()
	writeVariant(t, wd, "base/workflow", "b", "B")
	writeVariant(t, wd, "base/workflow", "a", "A")
	writeVariant(t, wd, "base/workflow", "default", "ignored")
	roots := VariantRoots(wd)

	got := DiscoverVariants("base/workflow", roots)
	require.Len(t, got, 2)
	assert.Equal(t, "base/workflow#a", got[0].ID)
	assert.Equal(t, "base/workflow#b", got[1].ID)
	assert.Empty(t, DiscoverVariants("base/identity", roots))

	all := DiscoverAllVariants(roots)
	assert.Len(t, all, 2)
}

func TestVariantProjectWinsOverGlobal(t *testing.T) {
	proj, glob := t.TempDir(), t.TempDir()
	writeVariant(t, proj, "base/workflow", "x", "project")
	writeVariant(t, glob, "base/workflow", "x", "global")
	writeVariant(t, glob, "base/workflow", "y", "global-only")
	roots := []string{
		filepath.Join(proj, ".pando", "prompts", "variants"),
		filepath.Join(glob, ".pando", "prompts", "variants"),
	}
	got := DiscoverVariants("base/workflow", roots)
	require.Len(t, got, 2)
	assert.True(t, strings.HasPrefix(got[0].Path, proj), "project variant must win on name clash")
}

func TestBuilderRendersSelectedVariantWithDataAndLuaHook(t *testing.T) {
	wd := t.TempDir()
	writeVariant(t, wd, "base/environment", "compact", "COMPACT-ENV dir={{.WorkingDir}} date={{.Date}} platform={{.Platform}}")

	script := filepath.Join(t.TempDir(), "hooks.lua")
	require.NoError(t, os.WriteFile(script, []byte(`
-- The hook input lives under ctx.parameters; a top-level section_content in the
-- returned table is what the builder reads back.
function hook_template_section(ctx)
  if ctx.parameters.section_name == "base/environment" then
    ctx.section_content = ctx.parameters.section_content .. "\n[hooked]"
  end
  return ctx
end
`), 0o644))
	lua, err := luaengine.NewFilterManager(script, 2*time.Second, true)
	require.NoError(t, err)
	t.Cleanup(lua.Close)

	ev := &fakeVariantEvaluator{choose: map[string]string{"base/environment": "compact"}}
	out, err := variantBuilder(t, wd, ev, lua).Build(sessionCtx())
	require.NoError(t, err)

	assert.Contains(t, out, "COMPACT-ENV dir="+wd+" date=9/29/2026 platform=linux", "variant rendered with the normal data")
	assert.Contains(t, out, "[hooked]", "variant passes through hook_template_section")
	assert.NotContains(t, out, "<env>", "embedded environment template must not be used")
	assert.Equal(t, []string{"base/environment"}, ev.calls, "only sections with variant files are selected")
}

func TestBuilderDefaultVariantAndRemovedDirUseEmbeddedTemplate(t *testing.T) {
	wd := t.TempDir()
	path := writeVariant(t, wd, "base/environment", "compact", "COMPACT-ENV")

	// Evaluator picks the default variant: embedded template.
	ev := &fakeVariantEvaluator{}
	out, err := variantBuilder(t, wd, ev, nil).Build(sessionCtx())
	require.NoError(t, err)
	assert.Contains(t, out, "<env>")
	assert.NotContains(t, out, "COMPACT-ENV")

	// Even a persisted choice of the variant falls back once the file is gone.
	require.NoError(t, os.RemoveAll(filepath.Join(wd, ".pando")))
	_ = path
	ev = &fakeVariantEvaluator{choose: map[string]string{"base/environment": "compact"}}
	out, err = variantBuilder(t, wd, ev, nil).Build(sessionCtx())
	require.NoError(t, err)
	assert.Contains(t, out, "<env>")
	assert.Empty(t, ev.calls, "no variants on disk: the evaluator is not consulted")
}

func TestBuilderBrokenVariantFallsBackToDefault(t *testing.T) {
	wd := t.TempDir()
	writeVariant(t, wd, "base/environment", "broken", "{{.Nope")
	ev := &fakeVariantEvaluator{choose: map[string]string{"base/environment": "broken"}}
	out, err := variantBuilder(t, wd, ev, nil).Build(sessionCtx())
	require.NoError(t, err)
	assert.Contains(t, out, "<env>")
}

func TestBuilderWithoutSessionUsesDefault(t *testing.T) {
	wd := t.TempDir()
	writeVariant(t, wd, "base/environment", "compact", "COMPACT-ENV")
	ev := &fakeVariantEvaluator{choose: map[string]string{"base/environment": "compact"}}
	out, err := variantBuilder(t, wd, ev, nil).Build(context.Background())
	require.NoError(t, err)
	assert.Contains(t, out, "<env>")
	assert.Empty(t, ev.calls)
}
