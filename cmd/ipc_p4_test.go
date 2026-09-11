// Copyright 2025 The Pando Authors. All rights reserved.
// Use of this source code is governed by a MIT-style license.

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/cronjob"
	"github.com/digiogithub/pando/internal/db"
	"github.com/digiogithub/pando/internal/instanceregistry"
	"github.com/digiogithub/pando/internal/ipc"
	ipcruntime "github.com/digiogithub/pando/internal/ipc/runtime"
	rag "github.com/digiogithub/pando/internal/rag"
	"github.com/digiogithub/pando/internal/rag/kb"
)

// TestP4EntrypointsSourceShape pins the P4 wiring of the entrypoints that
// cannot be driven end-to-end here (they call the heavy app.New).
func TestP4EntrypointsSourceShape(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(b)
	}
	mustContain := func(file, body string, needles ...string) {
		t.Helper()
		for _, n := range needles {
			if !strings.Contains(body, n) {
				t.Errorf("%s is missing %q", file, n)
			}
		}
	}
	ipcRole := regexp.MustCompile(`IPCRole:\s+rt\.Role`)

	agui := read("agui_serve.go")
	mustContain("agui_serve.go", agui,
		"ipcruntime.Bootstrap(ctx, cwd, instanceID)",
		"DBQuerier:   rt.Querier",
		"wireIPC(ctx, rt, pandoApp, instanceID, cwd, instanceregistry.ModeAGUI, wireOptions{})",
		"shutdownEntrypointOrdered(pandoApp.Shutdown, unwireIPC, rt.Cleanup)",
		"signal.NotifyContext",
	)
	if !ipcRole.MatchString(agui) {
		t.Error("agui_serve.go does not pass IPCRole: rt.Role to app.New")
	}

	cron := read("cronjob.go")
	mustContain("cronjob.go", cron,
		"ipcruntime.BootstrapWithOptions(",
		"AllowKillStalePrimary: false",
		"instanceregistry.ModeCronJob",
		"AcceptDelegations: &acceptDelegations",
		"shutdownEntrypointOrdered(a.Shutdown, unwireIPC, rt.Cleanup)",
	)
	if n := len(regexp.MustCompile(`OneShot:\s+true`).FindAllString(cron, -1)); n != 2 {
		t.Errorf("cronjob.go sets OneShot: true %d times, want 2 (AppOptions and wireOptions)", n)
	}
	if !ipcRole.MatchString(cron) {
		t.Error("cronjob.go does not pass IPCRole: rt.Role to app.New")
	}

	// No P4 entrypoint or one-shot CLI opens the database with db.Connect()
	// (migrations from a possibly different binary) any more.
	for _, file := range []string{"agui_serve.go", "cronjob.go", "kb.go", "db.go", "project.go", "design.go"} {
		if strings.Contains(read(file), "db.Connect()") {
			t.Errorf("%s still calls db.Connect()", file)
		}
	}
	for _, file := range []string{"kb.go", "db.go", "project.go", "design.go"} {
		if !strings.Contains(read(file), "db.ConnectCLI()") {
			t.Errorf("%s does not use db.ConnectCLI()", file)
		}
	}

	// The maintenance RPCs are registered in the shared primary wiring, so a
	// promoted secondary gets them too.
	mustContain("ipc_wiring.go", read("ipc_wiring.go"), "registerPrimaryMaintenanceHandlers(bus, pandoApp)")

	// Every cron-config write path (the three cron API handlers) propagates to
	// the primary.
	handlers := read("../internal/api/handlers_cronjobs.go")
	if n := strings.Count(handlers, "s.reloadCronJobsEverywhere(r.Context())"); n != 3 {
		t.Errorf("handlers_cronjobs.go propagates cron edits from %d handlers, want 3 (create, update, delete)", n)
	}
	if n := strings.Count(handlers, "config.UpdateCronJobs("); n != 3 {
		t.Errorf("handlers_cronjobs.go has %d config.UpdateCronJobs calls, want 3; a new write path must propagate too", n)
	}
	mustContain("handlers_cronjobs.go", handlers, "s.app.ForwardCronJobsToPrimary(")
}

func TestShutdownEntrypointOrdered(t *testing.T) {
	var got []string
	shutdownEntrypointOrdered(
		func() { got = append(got, "shutdown-app") },
		func() { got = append(got, "unwire-ipc") },
		func() { got = append(got, "cleanup-runtime") },
	)
	want := []string{"shutdown-app", "unwire-ipc", "cleanup-runtime"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestDecodeCronJobReloadParams(t *testing.T) {
	jobs := config.CronJobsConfig{Enabled: true, Jobs: []config.CronJob{{Name: "a", Schedule: "0 1 * * *", Prompt: "p", Enabled: true}}}
	inner, _ := json.Marshal(jobs)
	params, _ := json.Marshal(map[string]json.RawMessage{"cron_jobs": inner})

	got, err := decodeCronJobReloadParams(params)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled || len(got.Jobs) != 1 || got.Jobs[0].Name != "a" || got.Jobs[0].Schedule != "0 1 * * *" {
		t.Fatalf("decoded = %+v, want the sent config", got)
	}

	for _, bad := range []string{"", `{}`, `{"cron_jobs": 5}`, `not json`} {
		if _, err := decodeCronJobReloadParams(json.RawMessage(bad)); err == nil {
			t.Errorf("decode(%q) accepted invalid params", bad)
		}
	}
}

// TestCronJobReloadRPCReachesPrimary drives the full cronjob.reload path over a
// real bus: a secondary hands a cron configuration to the primary, whose
// running scheduler picks it up.
func TestCronJobReloadRPCReachesPrimary(t *testing.T) {
	resetIPCTestGlobals(t)
	project := isolatedIPCProject(t)
	ctx := context.Background()

	rtP, err := ipcruntime.Bootstrap(ctx, project, "p4-test-cron-primary")
	if err != nil {
		t.Fatalf("Bootstrap (primary): %v", err)
	}
	t.Cleanup(rtP.Cleanup)
	primaryApp := bareAppForIPCTest(rtP)
	svc := cronjob.NewService(nil, project, nil)
	if err := svc.Start(ctx, config.CronJobsConfig{}); err != nil {
		t.Fatalf("cron Start: %v", err)
	}
	t.Cleanup(svc.Stop)
	primaryApp.CronService = svc
	t.Cleanup(wireIPC(ctx, rtP, primaryApp, "p4-test-cron-primary", project, instanceregistry.ModeTUI, wireOptions{}))
	// Gate on the primary's ROUTER actually answering before anything is
	// forwarded to it: wirePrimary only logs and carries on when it cannot
	// bind, so without this the forward below fails with a bare "connection
	// refused" instead of naming the real cause.
	waitForPrimaryBus(t, ctx, fmt.Sprintf("tcp://127.0.0.1:%d", rtP.RPCPort))

	rtS, err := ipcruntime.Bootstrap(ctx, project, "p4-test-cron-secondary")
	if err != nil {
		t.Fatalf("Bootstrap (secondary): %v", err)
	}
	t.Cleanup(rtS.Cleanup)
	if rtS.Role != ipcruntime.RoleSecondary {
		t.Fatalf("role = %s, want secondary", rtS.Role)
	}
	secApp := bareAppForIPCTest(rtS)
	t.Cleanup(wireIPC(ctx, rtS, secApp, "p4-test-cron-secondary", project, instanceregistry.ModeWebUI, wireOptions{}))

	jobs := config.CronJobsConfig{Enabled: true, Jobs: []config.CronJob{
		{Name: "p4-rpc", Schedule: "30 4 * * *", Prompt: "from the secondary", Enabled: true},
	}}
	forwarded, err := secApp.ForwardCronJobsToPrimary(ctx, jobs)
	if err != nil || !forwarded {
		t.Fatalf("ForwardCronJobsToPrimary: forwarded=%v err=%v", forwarded, err)
	}

	listed := svc.ListJobs()
	if len(listed) != 1 || listed[0].Name != "p4-rpc" || listed[0].NextRun.IsZero() {
		t.Fatalf("primary scheduler jobs = %+v, want p4-rpc scheduled", listed)
	}

	// The primary itself has nobody to forward to.
	if forwarded, err := primaryApp.ForwardCronJobsToPrimary(ctx, jobs); forwarded || err != nil {
		t.Fatalf("primary ForwardCronJobsToPrimary: forwarded=%v err=%v, want false/nil", forwarded, err)
	}
}

// TestKBRelinkForwardsToPrimaryElseRunsLocally: `pando kb relink` runs
// in-process when no instance holds the lock, and on the primary (kb.relink
// RPC) when one does.
func TestKBRelinkForwardsToPrimaryElseRunsLocally(t *testing.T) {
	resetIPCTestGlobals(t)
	project := isolatedIPCProject(t)
	config.Get().Remembrances.KBWikiLinks = true
	ctx := context.Background()

	// Create and migrate the database, then seed one linked document.
	conn, err := db.Connect()
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO kb_documents (file_path, content) VALUES ('a.md', 'see [[b.md]]')`); err != nil {
		t.Fatalf("seed a.md: %v", err)
	}
	_ = conn.Close()

	// 1. No instance running: local fallback.
	res, forwarded, err := runKBRelink(ctx, project, false)
	if err != nil {
		t.Fatalf("runKBRelink (local): %v", err)
	}
	if forwarded || res.Documents != 1 || res.Links == 0 {
		t.Fatalf("local relink: forwarded=%v res=%+v, want a local pass over a.md", forwarded, res)
	}

	// 2. A primary is running: forwarded over kb.relink.
	rtP, err := ipcruntime.Bootstrap(ctx, project, "p4-test-kb-primary")
	if err != nil {
		t.Fatalf("Bootstrap (primary): %v", err)
	}
	t.Cleanup(rtP.Cleanup)
	primaryApp := bareAppForIPCTest(rtP)
	primaryApp.Remembrances = &rag.RemembrancesService{KB: kb.NewKBStore(rtP.SQLDB, nil, 0, 0)}
	t.Cleanup(wireIPC(ctx, rtP, primaryApp, "p4-test-kb-primary", project, instanceregistry.ModeTUI, wireOptions{}))
	// See the note in TestCronJobReloadRPCReachesPrimary: kb.relink is
	// forwarded over this bus, so it must be confirmed up first.
	waitForPrimaryBus(t, ctx, fmt.Sprintf("tcp://127.0.0.1:%d", rtP.RPCPort))

	if _, err := rtP.SQLDB.Exec(`INSERT INTO kb_documents (file_path, content) VALUES ('b.md', 'back to [[a.md]]')`); err != nil {
		t.Fatalf("seed b.md: %v", err)
	}

	res, forwarded, err = runKBRelink(ctx, project, false)
	if err != nil {
		t.Fatalf("runKBRelink (forwarded): %v", err)
	}
	if !forwarded || res.Documents != 1 || res.Candidates != 1 {
		t.Fatalf("forwarded relink: forwarded=%v res=%+v, want the primary to link only b.md", forwarded, res)
	}

	res, forwarded, err = runKBRelink(ctx, project, true)
	if err != nil {
		t.Fatalf("runKBRelink (forwarded, force): %v", err)
	}
	if !forwarded || res.Documents != 2 {
		t.Fatalf("forwarded forced relink: forwarded=%v res=%+v, want both documents relinked", forwarded, res)
	}
}

// TestWireIPCOneShotSecondaryIsNotPromoted: a one-shot secondary (`cronjob
// run`) never takes over when the primary hands over. An armed secondary
// promotes within ~0.2 s of a graceful handover (P1/P3 smoke tests); this one
// must leave the lock free.
func TestWireIPCOneShotSecondaryIsNotPromoted(t *testing.T) {
	resetIPCTestGlobals(t)
	project := isolatedIPCProject(t)
	ctx := context.Background()

	rtP, err := ipcruntime.Bootstrap(ctx, project, "p4-test-oneshot-primary")
	if err != nil {
		t.Fatalf("Bootstrap (primary): %v", err)
	}
	t.Cleanup(rtP.Cleanup)
	primaryApp := bareAppForIPCTest(rtP)
	t.Cleanup(wireIPC(ctx, rtP, primaryApp, "p4-test-oneshot-primary", project, instanceregistry.ModeTUI, wireOptions{}))

	rtS, err := ipcruntime.Bootstrap(ctx, project, "p4-test-oneshot-secondary")
	if err != nil {
		t.Fatalf("Bootstrap (secondary): %v", err)
	}
	t.Cleanup(rtS.Cleanup)
	if rtS.Role != ipcruntime.RoleSecondary {
		t.Fatalf("role = %s, want secondary", rtS.Role)
	}
	secApp := bareAppForIPCTest(rtS)
	acceptDelegations := false
	t.Cleanup(wireIPC(ctx, rtS, secApp, "p4-test-oneshot-secondary", project, instanceregistry.ModeCronJob, wireOptions{
		AcceptDelegations: &acceptDelegations,
		OneShot:           true,
	}))

	// Graceful handover: release the lock, announce instance.shutdown, close.
	rtP.Cleanup()
	time.Sleep(1500 * time.Millisecond)

	if secApp.IsIPCPrimary() {
		t.Fatal("one-shot secondary promoted itself")
	}
	isPrimary, _, lockFile, err := ipc.AcquireLock(project, "p4-test-oneshot-probe", rtS.PubPort, rtS.RPCPort)
	if err != nil || !isPrimary {
		t.Fatalf("lock not free after the handover (one-shot secondary took it?): isPrimary=%v err=%v", isPrimary, err)
	}
	ipc.ReleaseLock(lockFile)
}
