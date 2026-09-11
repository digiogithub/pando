package cronjob

import (
	"context"
	"testing"

	"github.com/digiogithub/pando/internal/config"
)

func testCronJobs() config.CronJobsConfig {
	return config.CronJobsConfig{
		Enabled: true,
		Jobs: []config.CronJob{
			{Name: "nightly", Schedule: "0 3 * * *", Prompt: "summarise the day", Enabled: true},
		},
	}
}

// An IPC secondary never starts the scheduler, but explicit actions (list,
// run now) must still see the configured jobs, and a hot reload from the
// WebUI must not pre-schedule entries a later Start would duplicate.
func TestUnstartedServiceReadsLiveConfigAndSchedulesNothing(t *testing.T) {
	s := NewService(nil, t.TempDir(), nil)
	s.configJobs = testCronJobs

	if err := s.Reload(testCronJobs()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if n := len(s.cron.Entries()); n != 0 {
		t.Fatalf("unstarted service scheduled %d entries, want 0", n)
	}

	jobs := s.ListJobs()
	if len(jobs) != 1 || jobs[0].Name != "nightly" {
		t.Fatalf("ListJobs = %+v, want the configured job", jobs)
	}
	s.mu.RLock()
	_, ok := s.jobByNameLocked("NIGHTLY")
	s.mu.RUnlock()
	if !ok {
		t.Fatal("RunNow lookup does not find a configured job on an unstarted service")
	}

	// An invalid configuration is still rejected.
	bad := testCronJobs()
	bad.Jobs[0].Prompt = ""
	if err := s.Reload(bad); err == nil {
		t.Fatal("Reload accepted an invalid configuration")
	}
}

func TestStartAfterReloadSchedulesEachJobOnce(t *testing.T) {
	s := NewService(nil, t.TempDir(), nil)
	s.configJobs = testCronJobs
	if err := s.Reload(testCronJobs()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, testCronJobs()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := len(s.cron.Entries()); n != 1 {
		t.Fatalf("Start scheduled %d entries, want 1", n)
	}
	if err := s.Reload(testCronJobs()); err != nil {
		t.Fatalf("Reload while started: %v", err)
	}
	if n := len(s.cron.Entries()); n != 1 {
		t.Fatalf("Reload while started left %d entries, want 1", n)
	}

	s.Stop()
	if n := len(s.cron.Entries()); n != 0 {
		t.Fatalf("Stop left %d entries, want 0", n)
	}
	s.Stop() // idempotent
}
