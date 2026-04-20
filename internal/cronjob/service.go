package cronjob

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/pubsub"
	mesnadamodels "github.com/digiogithub/pando/pkg/mesnada/models"
	"github.com/robfig/cron/v3"
)

type Service struct {
	mu           sync.RWMutex
	cfg          config.CronJobsConfig
	cron         *cron.Cron
	entryIDs     map[string]cron.EntryID
	runner       *runner
	logger       *slog.Logger
	started      bool
	reloadCh     chan config.ConfigChangeEvent
	reloadCancel context.CancelFunc
	broker       *pubsub.Broker[CronJobFiredPayload]
}

func NewService(orchestrator OrchestratorClient, workDir string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	broker := pubsub.NewBroker[CronJobFiredPayload]()
	return &Service{
		entryIDs: make(map[string]cron.EntryID),
		runner:   newRunner(orchestrator, workDir, logger, broker),
		logger:   logger,
		cron:     cron.New(cron.WithParser(cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow))),
		cfg:      config.CronJobsConfig{},
		started:  false,
		broker:   broker,
	}
}

// Subscribe returns a channel that emits CronJobFiredPayload events whenever
// a cronjob dispatch succeeds. The channel is closed when ctx is cancelled.
func (s *Service) Subscribe(ctx context.Context) <-chan pubsub.Event[CronJobFiredPayload] {
	return s.broker.Subscribe(ctx)
}

func (s *Service) Start(ctx context.Context, cfg config.CronJobsConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return s.reloadLocked(cfg)
	}
	if err := config.ValidateCronJobsConfig(cfg); err != nil {
		return err
	}
	s.cfg = cloneCronJobsConfig(cfg)
	if err := s.scheduleLocked(); err != nil {
		return err
	}
	s.cron.Start()
	s.started = true

	reloadCtx, cancel := context.WithCancel(ctx)
	s.reloadCancel = cancel
	s.reloadCh = make(chan config.ConfigChangeEvent, 8)
	config.Bus.Subscribe(s.reloadCh)
	go s.watchConfigChanges(reloadCtx)
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	if s.reloadCancel != nil {
		s.reloadCancel()
		s.reloadCancel = nil
	}
	if s.reloadCh != nil {
		config.Bus.Unsubscribe(s.reloadCh)
		s.reloadCh = nil
	}
	ctx := s.cron.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
	}
	s.entryIDs = make(map[string]cron.EntryID)
	s.cfg = config.CronJobsConfig{}
	s.started = false
	if s.broker != nil {
		s.broker.Shutdown()
	}
}

func (s *Service) Reload(cfg config.CronJobsConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reloadLocked(cfg)
}

func (s *Service) RunNow(ctx context.Context, name string) (*mesnadamodels.Task, error) {
	s.mu.RLock()
	job, ok := s.jobByNameLocked(name)
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cronjob %q not found", name)
	}
	return s.runner.run(ctx, job)
}

func (s *Service) ListJobs() []JobStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]JobStatus, 0, len(s.cfg.Jobs))
	for _, job := range s.cfg.Jobs {
		status := JobStatus{CronJob: job}
		if entryID, ok := s.entryIDs[job.Name]; ok {
			status.NextRun = s.cron.Entry(entryID).Next
		}
		jobs = append(jobs, status)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return jobs
}

type JobStatus struct {
	config.CronJob
	NextRun time.Time `json:"nextRun,omitempty"`
}

func (s *Service) reloadLocked(cfg config.CronJobsConfig) error {
	if err := config.ValidateCronJobsConfig(cfg); err != nil {
		return err
	}
	s.cfg = cloneCronJobsConfig(cfg)
	for _, entryID := range s.entryIDs {
		s.cron.Remove(entryID)
	}
	s.entryIDs = make(map[string]cron.EntryID)
	if err := s.scheduleLocked(); err != nil {
		return err
	}
	s.logger.Info("cronjob_event=reload", "jobs_count", len(s.cfg.Jobs))
	return nil
}

func (s *Service) scheduleLocked() error {
	if !s.cfg.Enabled {
		return nil
	}
	for _, job := range s.cfg.Jobs {
		if !job.Enabled {
			continue
		}
		job := normalizeJob(job)
		s.logger.Info("cronjob_event=scheduled", "name", job.Name, "schedule", job.Schedule)
		entryID, err := s.cron.AddFunc(job.Schedule, func() {
			_, _ = s.runner.run(context.Background(), job)
		})
		if err != nil {
			return fmt.Errorf("schedule cronjob %q: %w", job.Name, err)
		}
		s.entryIDs[job.Name] = entryID
	}
	return nil
}

func (s *Service) watchConfigChanges(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-s.reloadCh:
			if !ok {
				return
			}
			if ev.Source == "cronjob" {
				continue
			}
			cfg := config.Get()
			if cfg == nil {
				continue
			}
			if err := s.Reload(cfg.CronJobs); err != nil {
				s.logger.Error("cronjob_event=error", "error", err)
			}
		}
	}
}

func (s *Service) jobByNameLocked(name string) (config.CronJob, bool) {
	for _, job := range s.cfg.Jobs {
		if strings.EqualFold(job.Name, name) {
			return normalizeJob(job), true
		}
	}
	return config.CronJob{}, false
}

func cloneCronJobsConfig(cfg config.CronJobsConfig) config.CronJobsConfig {
	clone := config.CronJobsConfig{Enabled: cfg.Enabled}
	clone.Jobs = make([]config.CronJob, len(cfg.Jobs))
	copy(clone.Jobs, cfg.Jobs)
	return clone
}

func normalizeJob(job config.CronJob) config.CronJob {
	job.Name = strings.TrimSpace(job.Name)
	job.Schedule = strings.TrimSpace(job.Schedule)
	job.Prompt = strings.TrimSpace(job.Prompt)
	job.Engine = strings.TrimSpace(job.Engine)
	job.Model = strings.TrimSpace(job.Model)
	job.WorkDir = strings.TrimSpace(job.WorkDir)
	job.Timeout = strings.TrimSpace(job.Timeout)
	return job
}
