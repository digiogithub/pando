# Database and multiple instances

Pando keeps sessions, messages, file history, the knowledge base, session events and the code index in
one SQLite file: `<data.directory>/pando.db`.

Since v1.3.0 every Pando process opened on that file (TUI, `pando serve`, `pando app`, the desktop app,
ACP, `mcp-server`, CLI commands) **reads and writes it directly**. There is no primary instance that owns
the writes and no write traffic over IPC.

## Engines

| Platform | Engine | How concurrent writers behave |
|---|---|---|
| Linux, macOS, BSD | [go-sqlite-multiwriter](https://github.com/digiogithub/go-sqlite-multiwriter) in multi-process mode | Every connection of every process writes at the same time on its own snapshot. Commits that touch the same pages are validated; the loser is retried automatically. |
| Windows | Stock SQLite, WAL, `BEGIN IMMEDIATE`, 10 s busy timeout | Writers take turns on SQLite's write lock. |

The multiwriter engine keeps recent commits in a log next to the database (`pando.db-mw`,
`pando.db-mw.000001`, `pando.db-mwlock`) and compacts them into `pando.db` in the background. When the
last process closes the database everything is in `pando.db` and the log files are removed. A process
killed at any point leaves nothing to repair: the next opener replays the log.

Pando also creates `pando.db.lock` (which processes have the database open) and `pando.db.migrate.lock`
(migrations run once when several instances start together). They are empty and safe to leave in place.

## What the IPC lock still does

`<workdir>/.pando/ipc.lock` elects a **leader** per working directory. The leader runs the jobs that
must not run twice — the code index and its file watcher, the evaluator's background sweeps — and serves
the IPC bus used by the instances browser, remote view and hot-peer delegation. Followers write the
database themselves; when the leader exits, a follower takes the leader role over.

## Upgrading from v1.2.x

- **Close every Pando instance before starting v1.3.0 for the first time, and run only v1.3.0+ on a
  database from then on.** An older binary opening `pando.db` while the multiwriter engine holds commits
  in its log would not see them and could overwrite them.
- Databases created with `pando db compact` (v1.2.x) use `auto_vacuum=INCREMENTAL`, which the
  multiwriter engine cannot open. The first v1.3.0 start converts the file once (a full `VACUUM`, about a
  minute for a 3 GB database, needing free disk space about the size of the database) and prints a
  notice while it runs.

## Compaction

```
pando db compact
```

runs a full `VACUUM`. It rewrites the whole file, so it needs every Pando instance using the database to
be closed and refuses to run otherwise. `/db-compact` inside a running instance explains this instead of
compacting. Under the multiwriter engine `auto_vacuum` stays `NONE`, so `--incremental` and
`--no-auto-vacuum` only apply on Windows.

## Emergency switch

`PANDO_DB_ENGINE=sqlite` makes a process use stock SQLite instead of the multiwriter engine. Use it only
when **every** process using the database is started with it: a stock opener refuses a database that a
multiwriter process has open (`pando.db-mw` present), but it cannot protect a database it opened first
from a multiwriter process that starts later.

## Do not

- open `pando.db` with the `sqlite3` shell or another tool while Pando is running (read the file only
  when every instance is closed);
- put the data directory on a network file system.
