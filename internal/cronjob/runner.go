package cronjob

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/pubsub"
	mesnadamodels "github.com/digiogithub/pando/pkg/mesnada/models"
)

type runner struct {
	orchestrator OrchestratorClient
	workDir      string
	logger       *slog.Logger
	broker       *pubsub.Broker[CronJobFiredPayload]
}

func newRunner(orchestrator OrchestratorClient, workDir string, logger *slog.Logger, broker *pubsub.Broker[CronJobFiredPayload]) *runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &runner{orchestrator: orchestrator, workDir: workDir, logger: logger, broker: broker}
}

func (r *runner) run(ctx context.Context, job config.CronJob) (*mesnadamodels.Task, error) {
	resolvedWorkDir, err := r.resolveWorkDir(job.WorkDir)
	if err != nil {
		r.logger.Error("cronjob_event=error", "name", job.Name, "error", err)
		return nil, err
	}

	if runningTaskID, ok := r.runningTaskID(job.Name); ok {
		r.logger.Warn("cronjob_event=skipped", "name", job.Name, "reason", "already_running", "task_id", runningTaskID)
		return nil, fmt.Errorf("cronjob %q already running with task %s", job.Name, runningTaskID)
	}

	tags := append([]string{"cronjob", "cronjob:" + job.Name}, job.Tags...)
	req := mesnadamodels.SpawnRequest{
		Prompt:     job.Prompt,
		WorkDir:    resolvedWorkDir,
		Model:      strings.TrimSpace(job.Model),
		Engine:     mesnadamodels.Engine(strings.TrimSpace(job.Engine)),
		Tags:       dedupeTags(tags),
		Timeout:    strings.TrimSpace(job.Timeout),
		Background: true,
	}

	task, err := r.orchestrator.Spawn(ctx, req)
	if err != nil {
		r.logger.Error("cronjob_event=error", "name", job.Name, "error", err)
		return nil, err
	}

	r.logger.Info("cronjob_event=fired", "name", job.Name, "task_id", task.ID)
	if r.broker != nil {
		r.broker.Publish(pubsub.CreatedEvent, CronJobFiredPayload{JobName: job.Name, TaskID: task.ID})
	}
	return task, nil
}

func (r *runner) runningTaskID(jobName string) (string, bool) {
	tasks, err := r.orchestrator.ListTasks(mesnadamodels.ListRequest{
		Status: []mesnadamodels.TaskStatus{mesnadamodels.TaskStatusRunning},
		Tags:   []string{"cronjob:" + jobName},
		Limit:  1,
	})
	if err != nil || len(tasks) == 0 {
		return "", false
	}
	return tasks[0].ID, true
}

func (r *runner) resolveWorkDir(jobWorkDir string) (string, error) {
	baseDir := strings.TrimSpace(r.workDir)
	if baseDir == "" {
		baseDir = "."
	}
	candidate := strings.TrimSpace(jobWorkDir)
	if candidate == "" {
		candidate = baseDir
	} else if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(baseDir, candidate)
	}
	resolved, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("workdir %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %q is not a directory", resolved)
	}
	return resolved, nil
}

func dedupeTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result
}
