package conclusion

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func intPtr(i int) *int { return &i }

func TestEnrich_ParsedBlockFillsMetadata(t *testing.T) {
	task := &models.Task{
		Status:      models.TaskStatusCompleted,
		ProjectPath: "/tmp/myproject",
		Output: `done
<pando:conclusion>
status: success
summary: All good.
</pando:conclusion>`,
	}
	c := Enrich(task, DelegationOptions{}, nil)
	if c == nil {
		t.Fatal("expected a conclusion")
	}
	if c.Status != "success" || c.Summary != "All good." {
		t.Errorf("parsed fields wrong: %+v", c)
	}
	if c.Synthesized {
		t.Error("should not be synthesized when block present")
	}
	if c.CapturedAt.IsZero() {
		t.Error("CapturedAt must be set")
	}
	if task.ProjectName != "myproject" {
		t.Errorf("ProjectName = %q, want myproject (filepath.Base)", task.ProjectName)
	}
}

func TestEnrich_MissingBlockSynthesizeCompleted(t *testing.T) {
	task := &models.Task{
		Status:     models.TaskStatusCompleted,
		OutputTail: "lots of work done here",
	}
	c := Enrich(task, DelegationOptions{SynthesizeFallback: true}, nil)
	if c == nil {
		t.Fatal("expected synthesized conclusion")
	}
	if c.Status != "success" {
		t.Errorf("status = %q, want success", c.Status)
	}
	if !c.Synthesized {
		t.Error("expected Synthesized=true")
	}
	if !strings.Contains(c.Summary, "work done") {
		t.Errorf("summary should derive from output tail, got %q", c.Summary)
	}
}

func TestEnrich_MissingBlockSynthesizeFailed(t *testing.T) {
	task := &models.Task{
		Status:   models.TaskStatusFailed,
		Error:    "boom",
		ExitCode: intPtr(2),
	}
	c := Enrich(task, DelegationOptions{SynthesizeFallback: true}, nil)
	if c == nil {
		t.Fatal("expected synthesized conclusion")
	}
	if c.Status != "failed" {
		t.Errorf("status = %q, want failed", c.Status)
	}
	if !strings.Contains(c.Summary, "boom") {
		t.Errorf("summary should include error, got %q", c.Summary)
	}
}

func TestEnrich_NoFallbackReturnsNil(t *testing.T) {
	task := &models.Task{
		Status:     models.TaskStatusCompleted,
		OutputTail: "no block here",
	}
	if c := Enrich(task, DelegationOptions{SynthesizeFallback: false}, nil); c != nil {
		t.Errorf("expected nil when no block and fallback disabled, got %+v", c)
	}
}

func TestEnrich_ResolverFillsIDAndName(t *testing.T) {
	task := &models.Task{
		Status:      models.TaskStatusCompleted,
		ProjectPath: "/tmp/canon",
		Output:      "<pando:conclusion>\nsummary: s\n</pando:conclusion>",
	}
	resolver := func(path string) (string, string) {
		if path == "/tmp/canon" {
			return "proj-123", "Display Name"
		}
		return "", ""
	}
	Enrich(task, DelegationOptions{}, resolver)
	if task.ProjectID != "proj-123" {
		t.Errorf("ProjectID = %q, want proj-123", task.ProjectID)
	}
	if task.ProjectName != "Display Name" {
		t.Errorf("ProjectName = %q, want Display Name", task.ProjectName)
	}
}

func TestEnrich_NilResolverDerivesBaseName(t *testing.T) {
	task := &models.Task{
		Status:      models.TaskStatusCompleted,
		ProjectPath: "/home/user/code/widget",
		Output:      "<pando:conclusion>\nsummary: s\n</pando:conclusion>",
	}
	Enrich(task, DelegationOptions{}, nil)
	if task.ProjectName != "widget" {
		t.Errorf("ProjectName = %q, want widget", task.ProjectName)
	}
	if task.ProjectID != "" {
		t.Errorf("ProjectID should stay empty with nil resolver, got %q", task.ProjectID)
	}
}

func TestEnrich_StatusDefaultedFromTerminalState(t *testing.T) {
	// Block present but status omitted: enricher defaults from task state.
	task := &models.Task{
		Status: models.TaskStatusFailed,
		Output: "<pando:conclusion>\nsummary: s\n</pando:conclusion>",
	}
	c := Enrich(task, DelegationOptions{}, nil)
	if c == nil || c.Status != "failed" {
		t.Errorf("expected defaulted status=failed, got %+v", c)
	}
}

func TestEnrich_NilTask(t *testing.T) {
	if c := Enrich(nil, DelegationOptions{SynthesizeFallback: true}, nil); c != nil {
		t.Errorf("expected nil for nil task, got %+v", c)
	}
}
