---
created_at: 2026-06-22T12:17:16.53545428Z
updated_at: 2026-06-22T12:17:31.958097051Z
tags:
    - fix
    - mesnada
    - delegation
    - data-race
    - concurrency
    - i18n
    - acp
    - store
    - tests
---
# Fix: E1 follow-ups — data races under -race + WebUI i18n for delegation metrics

Date: 2026-06-22. Follow-up to pando/changes/delegation_e1_metrics.md after review feedback: "fix the failing tests, and WebUI strings must not be hardcoded — need i18n files."

## 1. FileStore.Close() didn't wait for background saver (test flake)
internal/mesnada/store/store.go. Close() only closed closeCh and returned; backgroundSaver then did its final fs.save() asynchronously, writing tasks.json.tmp AFTER Close returned. Under -race this raced t.TempDir teardown → TestTryStartWarmGating "directory not empty". Fix: sync.WaitGroup (wg.Add(1) before goroutine, defer wg.Done() in saver, wg.Wait() inside closeOnce) so Close blocks until the final flush completes. Makes every orchestrator test using a temp store deterministic.

## 2. PandoACPAgent.conn data race (pre-existing)
internal/mesnada/acp/{agent.go,session_state.go}. SetConnection writes a.conn under sessionsMu, but sendAvailableCommandsUpdate (spawned in a goroutine by NewSession/SetConnection) read a.conn unlocked → race (TestPandoACPAgent_SetConnection_SynchronizesExistingSessions). Fix: new (*PandoACPAgent).getConn() reads a.conn under sessionsMu.RLock(); sendAvailableCommandsUpdate pins a local conn := a.getConn() for the nil check and conn.SessionUpdate.

## 3. Test logger bytes.Buffer race (test infra)
internal/mesnada/acp/agent_pando_test.go. Async sendAvailableCommandsUpdate goroutine writes the test logger while the test reads logs.String(); bare bytes.Buffer is not concurrency-safe (TestPandoACPAgent_SetConnection_BackfillsAvailableCommandsForExistingSessions). Fix: test-local syncBuffer (mutex-guarded Write/String); the Backfills test uses it.

## 4. Terminal outputBuf data race (pre-existing)
internal/mesnada/acp/client.go. cmd.Stdout/Stderr = &term.outputBuf let os/exec's copier goroutine write the buffer with no lock while TerminalOutput read term.outputBuf.String() (writes bypass term.mu) → race (TestTerminalLifecycle). Fix: outputBuf is now a self-locking lockedBuffer (own mutex guarding Write/String) so writer and reader share the buffer's lock.

## 5. WebUI delegation-metrics strings now i18n
web-ui OrchestratorView.tsx DelegationMetricsBar previously hardcoded English. Now uses useTranslation() + t('orchestrator.delegationMetrics.*'). New orchestrator.delegationMetrics namespace (warmHitRate, warmHits, warmFailures, coldFallbacks, capRejections, resurrections, liveInjections, title) added to all 7 locales (en,es,de,fr,pt,ja,zh), inserted after the top-level nav section.

## Verification
go build ./... + gofmt -l clean. go test -race ./internal/mesnada/... ./internal/project all green (acp, store, orchestrator, project incl. previously-failing TryStartWarmGating, SetConnection×2, TerminalLifecycle); -count=5/-count=3 for stability. Non-race suites green: mesnada/*, app, api, tui/page, project, config, llm/agent, llm/tools. WebUI tsc clean; all 7 locale JSONs valid with new keys.