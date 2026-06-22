package conclusion

import (
	"strings"
	"testing"

	"github.com/digiogithub/pando/pkg/mesnada/models"
)

func TestFormatForParentFull(t *testing.T) {
	task := &models.Task{
		ID:          "task-123",
		Engine:      models.EnginePando,
		Status:      models.TaskStatusCompleted,
		ProjectName: "myproj",
		ProjectPath: "/home/u/code/myproj",
		Conclusion: &models.Conclusion{
			Status:     "success",
			Summary:    "Implemented the feature and added tests.",
			FollowUp:   "Wire the settings UI.",
			Artifacts:  []string{"internal/foo.go", "internal/foo_test.go"},
			MemoryRefs: []string{"pando/changes/foo.md"},
			Confidence: 0.9,
		},
	}

	out := FormatForParent(task)

	for _, want := range []string{
		"task-123",
		"engine=pando",
		"project=myproj",
		"status=success",
		"confidence=0.90",
		"Implemented the feature",
		"follow_up: Wire the settings UI.",
		"artifacts: internal/foo.go, internal/foo_test.go",
		"memory_refs: pando/changes/foo.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatForParent output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestFormatForParentOmitsEmptyOptionalFields(t *testing.T) {
	task := &models.Task{
		ID:     "t1",
		Engine: models.EngineClaude,
		Status: models.TaskStatusCompleted,
		Conclusion: &models.Conclusion{
			Status:  "partial",
			Summary: "Did part of the work.",
		},
	}
	out := FormatForParent(task)
	if strings.Contains(out, "follow_up:") {
		t.Errorf("expected no follow_up line, got:\n%s", out)
	}
	if strings.Contains(out, "artifacts:") {
		t.Errorf("expected no artifacts line, got:\n%s", out)
	}
	if strings.Contains(out, "memory_refs:") {
		t.Errorf("expected no memory_refs line, got:\n%s", out)
	}
	if strings.Contains(out, "confidence=") {
		t.Errorf("expected no confidence when zero, got:\n%s", out)
	}
}

func TestFormatForParentNilConclusion(t *testing.T) {
	task := &models.Task{
		ID:          "t2",
		Engine:      models.EnginePando,
		Status:      models.TaskStatusFailed,
		ProjectName: "proj",
	}
	out := FormatForParent(task)
	if !strings.Contains(out, "no conclusion captured") {
		t.Errorf("expected nil-conclusion guard text, got: %s", out)
	}
	if !strings.Contains(out, "t2") || !strings.Contains(out, "status=failed") {
		t.Errorf("expected id and status in guard line, got: %s", out)
	}
}

func TestFormatForParentNilTask(t *testing.T) {
	if out := FormatForParent(nil); !strings.Contains(out, "no task") {
		t.Errorf("expected nil-task guard, got: %s", out)
	}
}

func TestFormatForParentProjectLabelFallback(t *testing.T) {
	// No ProjectName, but ProjectPath present -> base of path.
	task := &models.Task{
		ID:          "t3",
		ProjectPath: "/a/b/cool-project",
		Conclusion:  &models.Conclusion{Status: "success", Summary: "x"},
	}
	if out := FormatForParent(task); !strings.Contains(out, "project=cool-project") {
		t.Errorf("expected project base fallback, got: %s", out)
	}
}
