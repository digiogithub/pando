# Extension memory capability

Pando's remembrance layer — keyed memories (`remember`/`recall`) and knowledge-base
documents (`kb_add_document`) — can be observed and augmented by a compiled-in extension:

- **`MemorySink`** observes every committed write, so an extension can ship it somewhere else.
- **`RemembranceSearchWrapper`** decorates search, so an extension can merge another store's
  documents into local results.

Core owns the interfaces and the points that call them. Transport, batching, retry, spooling,
dedup and redaction rules belong to the extension. That boundary is the point: the policy for
what may leave the machine lives in one place, and it is not the place that wants to send.

> **This capability moves project content off the machine.** Everything below is designed
> around that. Read the gate section before enabling anything.

---

## The gate

Nothing reaches a sink unless an operator opens the gate twice — once for the capability, once
per scope:

```toml
[Extensions.Memory]
Enabled = true
Scopes  = ["project/", "user/"]
```

Rules, all of which default to "no":

| Setting | Meaning |
|---|---|
| `Enabled` | Master switch. Default `false`. |
| `Scopes` | Scope prefixes whose writes may be published. **An empty list publishes nothing**, however true `Enabled` is. There is no wildcard. |
| `Paths` | Optional narrowing to document path prefixes. Empty means no path restriction. |
| `Origins` | Optional narrowing by subsystem: `tool`, `api`, `sync`, `watcher`, `gc`. Empty means all of them except `remote`. |
| `DryRun` | Mark every event as a dry run. Sinks must do everything except send, and log what they would have sent. |
| `Mode` | `async` (default, bounded queue, drops when full) or `sync` (blocks the write path until every sink returns). |
| `QueueSize` | Async queue depth. Default 256. |
| `TimeoutMs` | Per-sink call timeout. Default 5000. |
| `WrapSearch` | Enables `RemembranceSearchWrapper`. Separate switch: reads leak the query even when writes are allowed. |

Two consequences worth stating out loud:

- **A scopeless document is not covered by `"project/"`.** Documents with no memory scope match
  the empty prefix, so sharing them means listing `""` explicitly. Sharing a scope nobody thought
  about is the one accident this design refuses to allow.
- **A `remote`-origin write is never published**, whatever `Origins` says. Pushing back what a
  remote store just sent is how two stores start echoing each other forever.

Putting `[Extensions.Memory]` in the *project* configuration is what makes the opt-in per project.

### Auditing before you open the tap

Turn on `DryRun` first. Sinks are required to log what they would have sent, after redaction. Read
that log, then turn it off. A sink that ignores `DryRun` is broken, not merely impolite.

---

## Writing a sink

```go
type Corp struct{ /* ... */ }

func (c *Corp) ExtensionInfo() extension.Info {
    return extension.Info{ID: "memory.sink.corp", License: extension.LicenseEnterprise, /* ... */}
}

func (c *Corp) OnMemoryWrite(ctx context.Context, ev extension.MemoryEvent) error {
    if ev.DryRun {
        c.log.Info("would send", "path", ev.Path, "bytes", len(ev.Content))
        return nil
    }
    c.queue.enqueue(ev) // return fast; slow work belongs on your own goroutine
    return nil
}
```

Contract points that are easy to get wrong:

- **Return promptly.** The call sits on the agent's write path. Core bounds it with
  `TimeoutMs`, but a sink that regularly needs the timeout is stalling the user's turn.
- **Returning an error changes nothing.** The local write already committed; the error is logged
  and counted. A sink cannot veto a write, and must not be written as if it could.
- **Delivery is best effort.** Async mode drops events when the queue is full. A sink that must
  not lose events persists them itself — that is what a spool is for.
- **`Metadata` is shared** with the other sinks in the fan-out. Copy it before you modify it.
- **Redact on the way in.** A secret redacted at send time has already been written to your spool
  in clear text.

### `MemoryEvent`

Deliberately wide from the first release: core and an enterprise module ship from two
repositories, so adding a field later means a coordinated release of both.

`Kind` (memory/document), `Op` (created/updated/deleted), `Scope`, `Key`, `Path`, `Content`,
`Tags`, `Embedding`, `Metadata`, `ProjectID`, `UserID`, `InstanceID`, `Origin`, `Timestamp`,
`DryRun`.

`ProjectID`/`UserID`/`InstanceID` are **attribution, not isolation keys**. A Pando instance has one
internal user; tenancy belongs to whatever store the sink talks to.

---

## The visibility requirement

A sink that ships data must be visible while it does. Implement `MemorySyncReporter`:

```go
func (c *Corp) MemorySyncStatus() extension.MemorySyncStatus {
    return extension.MemorySyncStatus{
        Active: true, DryRun: c.dryRun, Destination: "remembrances.corp.internal",
        Pending: c.queue.len(), Sent: c.sent, LastSyncAt: c.lastSync,
    }
}
```

`GET /api/v1/extensions/memory` returns the gate as configured, core's own counters, and every
sink's report. It answers on **every** build, with `enabled: false` on a standard one — a 404
would make "no such feature" and "feature switched off" the same unknown to the UI.

The WebUI renders that as a status-bar indicator (`MemorySyncIndicator`) whenever the capability
is active. A sink that does not implement the reporter is shown as *shipping, state unknown*,
never as idle.

---

## Wrapping search

```go
func (c *Corp) WrapRemembranceSearch(next extension.RemembranceSearcher) extension.RemembranceSearcher {
    if !c.searchEnabled { return next }   // returning next unchanged is how you opt out
    return &merged{next: next, remote: c.client}
}
```

- Wrapping happens once, at startup, and only when `WrapSearch = true`.
- Chaining is in registration order; the first registered wrapper ends up outermost.
- **Fall back, do not fail.** A corporate store being unreachable must cost the remote hits, never
  the agent's ability to recall anything at all. Core also catches a wrapper that returns an error
  and falls back to local results, but a wrapper that degrades on its own gives a better answer.
- **Label your hits.** Set `Remembrance.Source` on anything that did not come from the local
  store. Core surfaces it as the `remembrance_source` metadata key, which the search tool already
  renders. An unlabelled remote hit is indistinguishable from a local one, which is exactly what
  the visibility rule forbids.
- Scores from two stores are not comparable. A wrapper that merges them owns producing a ranking
  that means something; appending remote hits after local ones is the honest default.

One choke point covers every read: `KBStore.SearchDocumentsWithOptions`. The `kb_search_documents`
tool, hybrid search, the context enricher and memory injection all go through it.

---

## Where the emission points are

| Write | Published as |
|---|---|
| `KBStore.AddDocument` | document / created |
| `KBStore.UpdateDocument` | document / updated |
| `KBStore.DeleteDocument` | document / deleted |
| `KBStore.UpsertMemory` | memory / created or updated, with scope and key |

Publication happens **after** the write commits, so a sink can never report something that did not
happen. An update is one event, not a delete followed by a create, even though that is how the
store implements it. `UpsertMemory` suppresses the nested document write it delegates to, so one
write is reported once.

Origin is carried on the context (`kb.WithWriteOrigin`) and set by the filesystem sync, the
watcher and the memory GC. `kb.WithoutWriteObserver` suppresses publication entirely; the IPC
dispatcher uses it when applying a write another instance forwarded, which that instance already
published.

---

## Building

The capability is inert in a standard build: no sink is registered, both hooks stay nil, and the
status endpoint answers `enabled: false`. Compose a binary with a sink using `xpando` — see
[extension-builds.md](extension-builds.md).
