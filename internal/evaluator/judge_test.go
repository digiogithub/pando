package evaluator

import (
	"strings"
	"testing"
)

func TestRenderJudgePrompt_DefaultTemplate(t *testing.T) {
	meta := JudgeMeta{
		TemplateName:    "base/identity",
		TemplateVersion: 1,
		Corrections:     2,
		Tokens:          1500,
		Transcript:      "User: hello\nAssistant: hi\n",
	}
	result, err := renderJudgePrompt("", meta)
	if err != nil {
		t.Fatalf("renderJudgePrompt: %v", err)
	}
	if !strings.Contains(result, "base/identity") {
		t.Error("expected template name in prompt")
	}
	if !strings.Contains(result, "2") {
		t.Error("expected correction count in prompt")
	}
	if !strings.Contains(result, "hello") {
		t.Error("expected transcript in prompt")
	}
}

func TestRenderJudgePrompt_CustomTemplate(t *testing.T) {
	custom := "Session: {{.TemplateName}}, Corrections: {{.Corrections}}"
	meta := JudgeMeta{TemplateName: "my-tmpl", Corrections: 3}
	result, err := renderJudgePrompt(custom, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Session: my-tmpl, Corrections: 3" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestParseJudgeOutput_ValidJSON(t *testing.T) {
	raw := `{"reasoning":"good","key_points":["a","b"],"new_skill":"be concise","task_type":"code","confidence":0.85}`
	out, err := parseJudgeOutput(raw)
	if err != nil {
		t.Fatalf("parseJudgeOutput: %v", err)
	}
	if out.Reasoning != "good" {
		t.Errorf("expected reasoning='good', got %q", out.Reasoning)
	}
	if len(out.KeyPoints) != 2 {
		t.Errorf("expected 2 key points, got %d", len(out.KeyPoints))
	}
	if out.NewSkill != "be concise" {
		t.Errorf("expected new_skill='be concise', got %q", out.NewSkill)
	}
	if out.Confidence != 0.85 {
		t.Errorf("expected confidence=0.85, got %f", out.Confidence)
	}
	if out.TaskType != "code" {
		t.Errorf("expected task_type='code', got %q", out.TaskType)
	}
}

func TestParseJudgeOutput_JSONWrappedInText(t *testing.T) {
	raw := `Here is my analysis:
{"reasoning":"ok","key_points":[],"new_skill":"","task_type":"general","confidence":0.6}
End of response.`
	out, err := parseJudgeOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Reasoning != "ok" {
		t.Errorf("expected reasoning='ok', got %q", out.Reasoning)
	}
}

func TestParseJudgeOutput_InvalidJSON(t *testing.T) {
	raw := "sorry I cannot evaluate this"
	out, err := parseJudgeOutput(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return partial result with the raw text as reasoning
	if out == nil {
		t.Fatal("expected non-nil output for invalid JSON")
	}
	if out.Confidence != 0 {
		t.Errorf("expected Confidence=0 for invalid JSON, got %f", out.Confidence)
	}
}
