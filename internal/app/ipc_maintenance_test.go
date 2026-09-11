package app

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/cronjob"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/ipc"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
)

// TestOneShotStartsNoPrimaryServicesEvenAsPrimary: AppOptions.OneShot
// (`cronjob run`) keeps every primary-only service off, whatever the role and
// whatever the trigger.
func TestOneShotStartsNoPrimaryServicesEvenAsPrimary(t *testing.T) {
	names := []string{"code-index-watcher", "kb-watch", "kb-link-backfill", "memory-gc", "cron"}
	for _, role := range []ipcruntime.Role{ipcruntime.RolePrimary, ipcruntime.RoleSecondary} {
		a := &App{lifetimeCtx: context.Background(), oneShot: true}
		rec := newServiceRecorder(a, names...)

		a.applyStartupIPCRole(role, "cronjob")
		rec.assertCounts(t, 0, names...)

		if a.startPrimaryServices("promotion") {
			t.Errorf("role %s: startPrimaryServices started the services of a one-shot process", role)
		}
		rec.assertCounts(t, 0, names...)
	}
}

// loadIsolatedConfig loads a config from a throwaway project and HOME.
func loadIsolatedConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	project := t.TempDir()
	t.Chdir(project)
	config.ResetForTests()
	t.Cleanup(config.ResetForTests)
	if _, err := config.Load(project, false); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return project
}

// snapshotFiles records every regular file (path -> mtime) under the roots.
func snapshotFiles(t *testing.T, roots ...string) map[string]time.Time {
	t.Helper()
	out := map[string]time.Time{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, ierr := d.Info(); ierr == nil {
				out[path] = info.ModTime()
			}
			return nil
		})
	}
	return out
}

// TestApplyCronJobsFromPeerUpdatesMemoryAndScheduler is the primary side of
// cronjob.reload: the in-memory config and the running scheduler take the new
// jobs, and no config file is written or created (the sender already saved it).
func TestApplyCronJobsFromPeerUpdatesMemoryAndScheduler(t *testing.T) {
	project := loadIsolatedConfig(t)
	home := os.Getenv("HOME")

	svc := cronjob.NewService(nil, project, nil)
	if err := svc.Start(context.Background(), config.CronJobsConfig{}); err != nil {
		t.Fatalf("cron Start: %v", err)
	}
	t.Cleanup(svc.Stop)
	a := &App{CronService: svc}

	before := snapshotFiles(t, project, home)

	jobs := config.CronJobsConfig{Enabled: true, Jobs: []config.CronJob{
		{Name: "p4", Schedule: "0 3 * * *", Prompt: "nightly", Enabled: true},
	}}
	res, err := a.ApplyCronJobsFromPeer(jobs)
	if err != nil {
		t.Fatalf("ApplyCronJobsFromPeer: %v", err)
	}
	if res.Jobs != 1 || !res.Scheduler {
		t.Fatalf("result = %+v, want 1 job with a scheduler", res)
	}
	if got := config.Get().CronJobs; !got.Enabled || len(got.Jobs) != 1 || got.Jobs[0].Name != "p4" {
		t.Fatalf("in-memory cron config = %+v, want the applied one", got)
	}
	listed := svc.ListJobs()
	if len(listed) != 1 || listed[0].Name != "p4" || listed[0].NextRun.IsZero() {
		t.Fatalf("scheduler jobs = %+v, want p4 scheduled", listed)
	}

	after := snapshotFiles(t, project, home)
	if len(after) != len(before) {
		t.Fatalf("config files changed: before %v, after %v", before, after)
	}
	for path, mtime := range before {
		if !after[path].Equal(mtime) {
			t.Errorf("%s was rewritten by ApplyCronJobsFromPeer", path)
		}
	}

	// An invalid configuration is refused and changes nothing.
	bad := config.CronJobsConfig{Enabled: true, Jobs: []config.CronJob{
		{Name: "bad", Schedule: "not a schedule", Prompt: "x", Enabled: true},
	}}
	if _, err := a.ApplyCronJobsFromPeer(bad); err == nil {
		t.Fatal("ApplyCronJobsFromPeer accepted an invalid schedule")
	}
	if got := config.Get().CronJobs; len(got.Jobs) != 1 || got.Jobs[0].Name != "p4" {
		t.Fatalf("in-memory cron config changed by a refused reload: %+v", got)
	}
}

// TestForwardCronJobsToPrimaryIsNoopWithoutSecondaryIPC: on a primary, or a
// process without IPC, there is nobody to forward to.
func TestForwardCronJobsToPrimaryIsNoopWithoutSecondaryIPC(t *testing.T) {
	a := &App{}
	forwarded, err := a.ForwardCronJobsToPrimary(context.Background(), config.CronJobsConfig{})
	if forwarded || err != nil {
		t.Fatalf("no IPC: forwarded=%v err=%v, want false/nil", forwarded, err)
	}

	client, err := ipc.NewClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	primary := &App{ipcClient: client, ipcRPCPort: 1}
	primary.ipcPrimary.Store(true)
	forwarded, err = primary.ForwardCronJobsToPrimary(context.Background(), config.CronJobsConfig{})
	if forwarded || err != nil {
		t.Fatalf("primary: forwarded=%v err=%v, want false/nil", forwarded, err)
	}
}

// TestRelinkKB is the primary side of kb.relink: it rebuilds the link graph on
// the writer pool (bare store when Remembrances is off), and a secondary
// refuses instead of silently skipping.
func TestRelinkKB(t *testing.T) {
	conn, err := db.ConnectAt(filepath.Join(t.TempDir(), "pando.db"))
	if err != nil {
		t.Fatalf("ConnectAt: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := conn.Exec(`INSERT INTO kb_documents (file_path, content) VALUES
		('notes/a.md', 'see [[notes/b.md]]'),
		('notes/b.md', 'back to [[notes/a.md]] and [[missing]]')`); err != nil {
		t.Fatalf("seed documents: %v", err)
	}

	ctx := context.Background()
	a := &App{rwConn: conn}

	stats, err := a.RelinkKB(ctx, false)
	if err != nil {
		t.Fatalf("RelinkKB: %v", err)
	}
	if stats.Documents != 2 || stats.Links == 0 {
		t.Fatalf("first pass = %+v, want 2 documents linked", stats)
	}

	stats, err = a.RelinkKB(ctx, false)
	if err != nil {
		t.Fatalf("RelinkKB (repeat): %v", err)
	}
	if stats.Candidates != 0 {
		t.Fatalf("repeat pass = %+v, want no candidates left", stats)
	}

	stats, err = a.RelinkKB(ctx, true)
	if err != nil {
		t.Fatalf("RelinkKB (force): %v", err)
	}
	if stats.Documents != 2 || stats.Links == 0 {
		t.Fatalf("forced pass = %+v, want 2 documents relinked", stats)
	}

	client, err := ipc.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	secondary := &App{rwConn: conn, ipcClient: client}
	if _, err := secondary.RelinkKB(ctx, false); err == nil {
		t.Fatal("RelinkKB on an IPC secondary did not refuse")
	}
}
