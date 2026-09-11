"""
Multi-process tests for Pando's single-writer IPC topology (primary election,
failover/promotion, role-aware services, one-shot processes, and writes that
survive a handover).

These are real multi-process tests: they build the `pando` binary once and then
drive several genuine `pando mcp-server` / `pando acp` / `pando serve` /
`pando cronjob run` processes against one shared project database, exactly as
an editor-spawned MCP server, a TUI and a web UI would run side by side. They
implement phase P6 ("multi-process tests") of
`pando/plans/mcp_server_ipc_bootstrap.md`, covering what the unit tests in
./internal/ipc/... and ./cmd/... structurally cannot: the actual OS-level
handover between separate processes when one of them dies.

ISOLATION (mandatory, never optional)
-------------------------------------
Every scenario runs inside `unshare -Urmn` -- a private user, mount AND network
namespace -- with:
  * a tmpfs mounted over /tmp/pando-instances, so the instance registry is
    invisible to (and cannot disturb) the real one on this machine;
  * a private loopback, so the deterministic IPC ports derived from the project
    path can never collide with, connect to, or be connected to by a real
    Pando instance running on the host;
  * a throwaway $HOME and a throwaway project directory under a temp dir;
  * `[Data] Directory` set explicitly in the scratch `.pando.toml`.

The consequence is what matters: these tests can never read, write, lock, kill
or signal anything belonging to a real Pando instance, and every process they
touch is one they started themselves. They never look at the repository's own
.pando/ directory or .pando.toml.

HOW IT WORKS
------------
Each test method re-executes THIS FILE inside the namespace
(`--scenario <name>`), because the namespace has to be entered by a whole
process, not by a single call. The child runs the scenario, prints one
`PASS`/`FAIL` line per assertion, and exits non-zero if any failed. The parent
test method asserts the child exited 0 and, on failure, reports the child's
full output -- so a failing assertion reads normally in unittest output.

SCENARIOS
---------
  join            `pando mcp-server --no-http` starts first and is the primary
                  (real `initialize` over stdio); an ACP secondary joins; the
                  lock file PID, `pando ipc status` and the instance registry
                  all agree on who is primary and on both modes.
  stop_eof        The mcp-server primary is stopped by closing its stdin...
  stop_term       ...by SIGTERM...
  stop_kill       ...and by SIGKILL (no defers run; the kernel frees the flock,
                  so promotion waits out the ~15s heartbeat timeout).
                  In all three: exactly one of the two ACP secondaries is
                  promoted, the promoted one starts the primary-only background
                  services (P3) while the other never does, a session write
                  lands on the new primary, and a freshly started mcp-server
                  secondary's KB write is applied through it.
  concurrent      3 concurrent `mcp-server`s plus one ACP instance: exactly one
                  primary overall, and writes issued by all four land in the DB.
  cronjob         `pando cronjob run` next to a primary exits 0, starts nothing
                  and never promotes (P4 one-shot); and a cron edit made through
                  a `serve` secondary's REST API reaches the primary over the
                  `cronjob.reload` RPC.
  handover_write  A KB write issued while the primary gracefully hands over
                  succeeds on the new primary (P5 handover retry).

RUNNING THEM
------------
    python3 -m unittest tests.test_ipc_multiprocess -v          # all scenarios
    python3 -m unittest tests.test_ipc_multiprocess.IPCMultiProcessTest.test_join

This repository has no `tests/` runner and no CI job that executes this
directory (there is no pytest configuration, and the Go workflow runs only
`go test`), so these tests do NOT run in normal CI: they are run on demand, by
a developer or an agent working on the IPC code. They are also slow by nature
(each scenario starts several real Pando processes, and stop_kill deliberately
waits out a heartbeat timeout), which is the other reason they are opt-in.

They skip -- never fail -- when the environment cannot support them: no Go
toolchain, no `unshare`, or a kernel that refuses unprivileged user namespaces.
"""

import argparse
import glob
import json
import os
import shutil
import signal
import sqlite3
import ssl
import subprocess
import sys
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request

PANDO_ROOT = "/www/MCP/Pando/pando"
SELF = os.path.abspath(__file__)

# Per-scenario budget in the parent. Generous on purpose: every scenario starts
# several real Pando processes (app.New dominates), and stop_kill waits out the
# ~15s heartbeat timeout before a promotion can happen.
SCENARIO_TIMEOUT_SECONDS = 420

# Log markers emitted by the IPC/role code. Keep these in sync with
# internal/app/primary_services.go, internal/app/app.go and cmd/ipc_wiring.go.
PRIMARY_START = "IPC role: primary, starting primary-only background services"
SECONDARY_SKIP = "IPC role: secondary, primary-only background services skipped until promotion"
ONESHOT = "IPC role: one-shot process, primary-only background services disabled"
ONESHOT_NO_PROMOTE = "IPC: one-shot secondary, failover promotion not armed"
PROMOTED = "PromoteToPrimary complete"
CRON_SCHEDULED = "cronjob_event=scheduled"
RELOAD_APPLIED = "cronjob.reload: applied cron configuration from another instance"
RELOAD_SENT = "cronjob: saved cron configuration handed to the IPC primary"

# `[Data] Directory` is set explicitly: a config rewrite (the cron scenario
# performs one through the REST API) fills absent keys, and a blanked data
# directory would break every later process in the scenario.
PANDO_TOML = """
LogFile = '%(clilog)s'

[Data]
Directory = '.pando'

[Remembrances]
Enabled = true
KBAutoImport = false
KBWatch = false
KBWikiLinks = true
DocumentEmbeddingProvider = 'ollama'
DocumentEmbeddingModel = 'nomic-embed-text'
CodeEmbeddingProvider = 'ollama'
CodeEmbeddingModel = 'nomic-embed-text'
MemoryEnabled = true

[Mesnada]
Enabled = true

[CronJobs]
Enabled = true

[[CronJobs.Jobs]]
Name = 'p6job'
Schedule = '0 3 * * *'
Prompt = 'noop'
Engine = 'p6-nonexistent-engine'
Enabled = true
"""


# ---------------------------------------------------------------------------
# Parent side: build the binary, run each scenario inside the sandbox
# ---------------------------------------------------------------------------

_BUILD_DIR = None
PANDO_BIN = None


def _sandbox_available():
    """Report whether unprivileged user+mount+net namespaces actually work."""
    if shutil.which("unshare") is None:
        return False, "unshare(1) is not on PATH"
    probe = subprocess.run(
        ["unshare", "-Urmn", "true"], capture_output=True, text=True
    )
    if probe.returncode != 0:
        return False, (
            "unprivileged user namespaces are unavailable: "
            + (probe.stderr or probe.stdout).strip()
        )
    return True, ""


def setUpModule():
    """Build the pando binary once, into a temp dir (never into the repo)."""
    global _BUILD_DIR, PANDO_BIN

    if shutil.which("go") is None:
        raise unittest.SkipTest("go toolchain not found on PATH")
    ok, why = _sandbox_available()
    if not ok:
        raise unittest.SkipTest(
            "these tests refuse to run outside an isolated namespace (%s); "
            "they would otherwise share the real instance registry and IPC "
            "ports with live Pando processes" % why
        )

    _BUILD_DIR = tempfile.mkdtemp(prefix="pando-ipc-p6-")
    PANDO_BIN = os.path.join(_BUILD_DIR, "pando")
    result = subprocess.run(
        ["go", "build", "-o", PANDO_BIN, "."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(
            "failed to build the pando binary for the IPC multi-process tests:\n"
            + result.stdout
            + result.stderr
        )


def tearDownModule():
    if _BUILD_DIR and os.path.isdir(_BUILD_DIR):
        shutil.rmtree(_BUILD_DIR, ignore_errors=True)


class IPCMultiProcessTest(unittest.TestCase):
    """Each test runs one scenario in its own namespace and scratch project."""

    def _run_scenario(self, name):
        scratch = tempfile.mkdtemp(prefix="p6-%s-" % name, dir=_BUILD_DIR)
        # The namespace must be entered by a whole process, so the scenario
        # body re-executes this file inside it. Everything the scenario needs
        # is passed through the environment.
        inner = (
            "set -eu\n"
            "ip link set lo up\n"
            "mkdir -p /tmp/pando-instances\n"
            "mount -t tmpfs tmpfs /tmp/pando-instances\n"
            'exec python3 "$SELF" --scenario "$SCENARIO"\n'
        )
        cmd = [
            "unshare", "-Urmn",
            "env",
            "BIN=%s" % PANDO_BIN,
            "S=%s" % scratch,
            "SELF=%s" % SELF,
            "SCENARIO=%s" % name,
            "bash", "-c", inner,
        ]
        try:
            proc = subprocess.run(
                cmd, capture_output=True, text=True,
                timeout=SCENARIO_TIMEOUT_SECONDS,
            )
        except subprocess.TimeoutExpired as exc:
            self.fail(
                "scenario %r did not finish within %ds.\n--- output so far ---\n%s"
                % (name, SCENARIO_TIMEOUT_SECONDS,
                   (exc.output or b"").decode(errors="replace")
                   if isinstance(exc.output, bytes) else (exc.output or ""))
            )
        if proc.returncode != 0:
            self.fail(
                "scenario %r failed (exit %d).\n--- stdout ---\n%s\n--- stderr ---\n%s"
                % (name, proc.returncode, proc.stdout, proc.stderr[-4000:])
            )
        sys.stderr.write("\n[%s]\n%s" % (name, proc.stdout))

    def test_join(self):
        """mcp-server is primary, an ACP secondary joins; lock/status/registry agree."""
        self._run_scenario("join")

    def test_stop_primary_stdin_eof(self):
        """mcp-server primary stopped by stdin EOF: a secondary is promoted and can write."""
        self._run_scenario("stop_eof")

    def test_stop_primary_sigterm(self):
        """mcp-server primary stopped by SIGTERM: a secondary is promoted and can write."""
        self._run_scenario("stop_term")

    def test_stop_primary_sigkill(self):
        """mcp-server primary SIGKILLed: promotion still happens, within the heartbeat timeout."""
        self._run_scenario("stop_kill")

    def test_concurrent_instances(self):
        """3 mcp-servers + 1 ACP: exactly one primary, every instance's write lands."""
        self._run_scenario("concurrent")

    def test_cronjob_oneshot_and_reload(self):
        """`cronjob run` is a one-shot that never promotes; a cron edit reaches the primary."""
        self._run_scenario("cronjob")

    def test_write_during_handover(self):
        """A write issued during a graceful handover succeeds on the new primary."""
        self._run_scenario("handover_write")


# ---------------------------------------------------------------------------
# Child side: scenario helpers (only used inside the namespace)
# ---------------------------------------------------------------------------

BIN = os.environ.get("BIN", "")
S = os.environ.get("S", "")
PROJ = os.path.join(S, "proj") if S else ""
CLILOG = os.path.join(S, "cli.log") if S else ""
FAILS = []


def check(cond, msg):
    print(("PASS " if cond else "FAIL ") + msg, flush=True)
    if not cond:
        FAILS.append(msg)


def finish():
    print("RESULT:", "FAIL" if FAILS else "OK", FAILS, flush=True)
    sys.exit(1 if FAILS else 0)


def child_env():
    env = dict(os.environ)
    env["HOME"] = os.path.join(S, "home")
    env["XDG_CONFIG_HOME"] = ""
    return env


def text(path):
    try:
        with open(path, errors="replace") as handle:
            return handle.read()
    except FileNotFoundError:
        return ""


def count(path, marker):
    return text(path).count(marker)


def line_with(path, marker):
    for line in text(path).splitlines():
        if marker in line:
            return line.strip()
    return "<none>"


def wait_for(path, marker, timeout):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if marker in text(path):
            return True
        time.sleep(0.1)
    return False


def wait_for_any(paths, marker, timeout):
    """Poll several logs at once, returning the first that shows the marker.

    Waiting on one log and only then on the next would add that first log's
    full timeout to the measurement whenever the other process is the one that
    matched -- which is exactly what makes a promotion-latency assertion
    measure the harness instead of the product.
    """
    deadline = time.time() + timeout
    while time.time() < deadline:
        for path in paths:
            if marker in text(path):
                return path
        time.sleep(0.05)
    return None


def setup_project():
    os.makedirs(PROJ, exist_ok=True)
    os.makedirs(os.path.join(S, "home"), exist_ok=True)
    with open(os.path.join(PROJ, ".pando.toml"), "w") as handle:
        handle.write(PANDO_TOML % {"clilog": CLILOG})


def lock_info():
    raw = text(os.path.join(PROJ, ".pando", "ipc.lock")).strip()
    if not raw:
        return None
    try:
        return json.loads(raw)
    except ValueError:
        return {"raw": raw}


def registry_entries():
    entries = []
    for path in glob.glob("/tmp/pando-instances/*.json"):
        try:
            with open(path) as handle:
                entries.append(json.load(handle))
        except (OSError, ValueError):
            pass
    return entries


def wait_for_registry(predicate, timeout):
    deadline = time.time() + timeout
    while time.time() < deadline:
        entries = registry_entries()
        if predicate(entries):
            return entries
        time.sleep(0.2)
    return registry_entries()


def db_path():
    found = glob.glob(os.path.join(PROJ, ".pando", "**", "pando.db"), recursive=True)
    return found[0] if found else None


def wait_for_db(timeout=60):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if db_path():
            return True
        time.sleep(0.2)
    return False


def scalar(query, args=()):
    path = db_path()
    if not path:
        return None
    con = sqlite3.connect("file:%s?mode=ro" % path, uri=True, timeout=20)
    try:
        row = con.execute(query, args).fetchone()
        return row[0] if row else None
    finally:
        con.close()


def seed_memory(key):
    """Insert a memory row directly.

    Storing one through `remember` would embed its content, and this namespace
    deliberately has no route to an embedding provider. `forget` (delete by
    key) needs no embedder and takes the same always-forwarded write path, so
    it is what the scenarios use to prove a KB write reached the primary.
    """
    con = sqlite3.connect(db_path(), timeout=30)
    try:
        con.execute(
            "INSERT INTO kb_documents (file_path, content, memory_key) VALUES (?, ?, ?)",
            ("memory/" + key, "p6 " + key, key),
        )
        con.commit()
    finally:
        con.close()


def memory_rows(key):
    return scalar("SELECT COUNT(*) FROM kb_documents WHERE memory_key = ?", (key,))


def ipc_status():
    out = subprocess.run(
        [BIN, "ipc", "status", "--path", PROJ],
        cwd=PROJ, env=child_env(), capture_output=True, text=True, timeout=60,
    )
    return out.stdout


def run_cli(name, args, timeout=120):
    started = time.time()
    proc = subprocess.run(
        [BIN] + args, cwd=PROJ, env=child_env(), stdin=subprocess.DEVNULL,
        capture_output=True, timeout=timeout,
    )
    elapsed = time.time() - started
    with open(os.path.join(S, name + ".out"), "wb") as handle:
        handle.write(proc.stdout)
    with open(os.path.join(S, name + ".err"), "wb") as handle:
        handle.write(proc.stderr)
    return proc, elapsed


class Proc:
    """A Pando subprocess speaking line-delimited JSON-RPC on stdio."""

    def __init__(self, name, args):
        self.name = name
        self.p = subprocess.Popen(
            [BIN] + args, cwd=PROJ, env=child_env(),
            stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=open(os.path.join(S, name + ".err"), "wb"),
        )
        self.lines = []
        self._lock = threading.Lock()
        threading.Thread(target=self._read, daemon=True).start()

    @property
    def pid(self):
        return self.p.pid

    def _read(self):
        for raw in iter(self.p.stdout.readline, b""):
            line = raw.decode("utf-8", errors="replace").strip()
            if line:
                with self._lock:
                    self.lines.append(line)

    def send(self, obj):
        self.p.stdin.write((json.dumps(obj) + "\n").encode())
        self.p.stdin.flush()

    def wait_id(self, rid, timeout=60.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            with self._lock:
                snapshot = list(self.lines)
            for line in snapshot:
                try:
                    obj = json.loads(line)
                except ValueError:
                    continue
                if obj.get("id") == rid:
                    return obj
            time.sleep(0.02)
        return None

    def stop_eof(self, timeout=60):
        try:
            self.p.stdin.close()
        except OSError:
            pass
        return self._wait(timeout)

    def stop_signal(self, sig, timeout=60):
        self.p.send_signal(sig)
        return self._wait(timeout)

    def _wait(self, timeout):
        try:
            return self.p.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            self.p.kill()
            return self.p.wait()

    def kill(self):
        if self.p.poll() is None:
            self.p.kill()
            self.p.wait()


def mcp_init(mid=1):
    return {"jsonrpc": "2.0", "id": mid, "method": "initialize",
            "params": {"protocolVersion": "2024-11-05", "capabilities": {},
                       "clientInfo": {"name": "p6", "version": "0"}}}


def acp_init(mid=1):
    return {"jsonrpc": "2.0", "id": mid, "method": "initialize",
            "params": {"protocolVersion": 1, "clientCapabilities": {}}}


def acp_session_new(mid):
    return {"jsonrpc": "2.0", "id": mid, "method": "session/new",
            "params": {"cwd": PROJ, "mcpServers": []}}


def tool_call(mid, name, arguments):
    return {"jsonrpc": "2.0", "id": mid, "method": "tools/call",
            "params": {"name": name, "arguments": arguments}}


def tool_failed(resp):
    if resp is None or "error" in resp:
        return True
    return bool(resp.get("result", {}).get("isError"))


def start_mcp(name):
    """Start `pando mcp-server --no-http` and complete its MCP initialize."""
    proc = Proc(name, ["mcp-server", "--no-http", "--cwd", PROJ])
    proc.send(mcp_init())
    answered = proc.wait_id(1, timeout=90) is not None
    return proc, answered


def start_acp(name, log):
    """Start `pando acp` (a long-lived, promotion-eligible instance)."""
    proc = Proc(name, ["acp", "--cwd", PROJ, "--debug", "--log-file", log])
    proc.send(acp_init())
    answered = proc.wait_id(1, timeout=90) is not None
    return proc, answered


# ---------------------------------------------------------------------------
# Scenarios
# ---------------------------------------------------------------------------

def scenario_join():
    """mcp-server first (primary), then an ACP secondary; everyone agrees who is who."""
    setup_project()
    b_log = os.path.join(S, "b-acp.log")

    mcp, answered = start_mcp("a-mcp")
    check(answered, "mcp-server answered MCP initialize over stdio")
    check(wait_for(CLILOG, PRIMARY_START, 60),
          "mcp-server started as PRIMARY: " + line_with(CLILOG, PRIMARY_START))
    check((lock_info() or {}).get("pid") == mcp.pid,
          "the lock file names the mcp-server (pid %s, lock %s)" % (mcp.pid, (lock_info() or {}).get("pid")))

    acp, answered = start_acp("b-acp", b_log)
    check(answered, "the ACP instance answered initialize")
    check(wait_for(b_log, SECONDARY_SKIP, 60),
          "the ACP instance joined as a SECONDARY: " + line_with(b_log, SECONDARY_SKIP))
    check(count(b_log, PRIMARY_START) == 0, "the secondary started no primary-only services")
    check((lock_info() or {}).get("pid") == mcp.pid, "the mcp-server still holds the lock after the secondary joined")

    status = ipc_status()
    check("Lock:      held by instance" in status and str(mcp.pid) in status,
          "`pando ipc status` reports the mcp-server as the lock holder")
    check("Known instances for this path: 2" in status,
          "`pando ipc status` knows about both instances:\n" + status)
    check("Mode:        mcp" in status,
          "`pando ipc status` reports the primary's mode as mcp")

    entries = wait_for_registry(lambda e: len(e) == 2, 30)
    by_pid = {e.get("pid"): e for e in entries}
    check(len(entries) == 2, "the instance registry holds exactly 2 entries: %s" % entries)
    check(by_pid.get(mcp.pid, {}).get("mode") == "mcp" and by_pid.get(mcp.pid, {}).get("is_primary") is True,
          "registry: the mcp-server is mode=mcp, is_primary=true -> %s" % by_pid.get(mcp.pid))
    check(by_pid.get(acp.pid, {}).get("mode") == "acp" and by_pid.get(acp.pid, {}).get("is_primary") is False,
          "registry: the ACP instance is mode=acp, is_primary=false -> %s" % by_pid.get(acp.pid))
    check(sum(1 for e in entries if e.get("is_primary")) == 1, "exactly one instance claims to be primary")

    check(acp.stop_eof() == 0, "the ACP secondary exited 0")
    check(mcp.stop_eof() == 0, "the mcp-server primary exited 0")


def scenario_stop(mode):
    """Stop an mcp-server primary three ways; a secondary must take over and write.

    Two ACP secondaries are used so the test can prove that exactly ONE of them
    promotes and starts the primary-only services, while the other stays a
    secondary. The post-promotion KB write is issued by a freshly started
    mcp-server (ACP exposes no KB tools), which also proves a new process can
    still join and forward writes to the NEW primary.
    """
    setup_project()
    b_log = os.path.join(S, "b-acp.log")
    c_log = os.path.join(S, "c-acp.log")

    primary, answered = start_mcp("a-mcp")
    check(answered, "mcp-server (primary) answered MCP initialize")
    check(wait_for(CLILOG, PRIMARY_START, 60), "mcp-server is the primary")
    check(wait_for_db(60), "the primary created the project database")

    b, b_ok = start_acp("b-acp", b_log)
    c, c_ok = start_acp("c-acp", c_log)
    check(b_ok and c_ok, "both ACP secondaries answered initialize")
    check(wait_for(b_log, SECONDARY_SKIP, 60) and wait_for(c_log, SECONDARY_SKIP, 60),
          "both ACP instances joined as secondaries")
    check(count(b_log, PRIMARY_START) == 0 and count(c_log, PRIMARY_START) == 0,
          "neither secondary started the primary-only services while the primary was alive")

    key = "p6.after." + mode
    seed_memory(key)
    sessions_before = scalar("SELECT COUNT(*) FROM sessions") or 0

    # SIGKILL runs no defers: the kernel releases the flock, and the surviving
    # secondaries only notice once the ~15s heartbeat timeout expires.
    promote_budget = 90 if mode == "kill" else 30
    started = time.time()
    if mode == "eof":
        rc = primary.stop_eof(timeout=90)
        check(rc == 0, "the primary exited 0 after stdin EOF (ordered handover ran)")
    elif mode == "term":
        rc = primary.stop_signal(signal.SIGTERM, timeout=90)
        check(rc == 0, "the primary exited 0 after SIGTERM (ordered handover ran)")
    else:
        rc = primary.stop_signal(signal.SIGKILL, timeout=90)
        check(rc == -signal.SIGKILL, "the primary was SIGKILLed (exit %s)" % rc)

    winner_log = wait_for_any([b_log, c_log], PROMOTED, promote_budget)
    elapsed = time.time() - started
    promoted_b = winner_log == b_log
    promoted_c = winner_log == c_log
    check(winner_log is not None,
          "a secondary was promoted after the %s stop (%.2fs)" % (mode, elapsed))
    if winner_log is None:
        finish()
    # Give the loser a moment to (wrongly) promote too before ruling it out.
    time.sleep(1.0)
    check(not (PROMOTED in text(b_log) and PROMOTED in text(c_log)),
          "exactly one secondary was promoted, not both")
    if mode in ("eof", "term"):
        check(elapsed < 20, "the graceful stop promoted quickly (%.2fs, no heartbeat wait)" % elapsed)
    else:
        print("NOTE SIGKILL promotion took %.2fs (heartbeat timeout path)" % elapsed, flush=True)

    winner_log, loser_log = (b_log, c_log) if promoted_b else (c_log, b_log)
    winner, loser = (b, c) if promoted_b else (c, b)
    check(wait_for(winner_log, PRIMARY_START, 30),
          "the promoted instance started the primary-only services: " + line_with(winner_log, PRIMARY_START))
    check("trigger=promotion" in line_with(winner_log, PRIMARY_START),
          "it started them with trigger=promotion")
    check(count(loser_log, PRIMARY_START) == 0,
          "the instance that was NOT promoted still runs no primary-only services")
    check((lock_info() or {}).get("pid") == winner.pid,
          "the lock file now names the promoted instance (pid %s)" % winner.pid)

    # A session write proves the new primary can actually write.
    winner.send(acp_session_new(50))
    resp = winner.wait_id(50, timeout=90)
    check(resp is not None and "error" not in resp,
          "session/new on the new primary succeeded: %s" % json.dumps(resp)[:200])
    sessions_after = scalar("SELECT COUNT(*) FROM sessions") or 0
    check(sessions_after > sessions_before,
          "the session row was written (%s -> %s)" % (sessions_before, sessions_after))

    # A KB write from a NEW mcp-server secondary, forwarded to the new primary.
    late, late_ok = start_mcp("d-mcp")
    check(late_ok, "a new mcp-server joined after the promotion and answered initialize")
    late.send(tool_call(60, "forget", {"key": key}))
    resp = late.wait_id(60, timeout=120)
    check(not tool_failed(resp), "its KB write succeeded through the new primary: %s" % json.dumps(resp)[:200])
    check(memory_rows(key) == 0, "the KB write was applied to the database")

    check(late.stop_eof() == 0, "the late mcp-server exited 0")
    check(loser.stop_eof() == 0, "the remaining secondary exited 0")
    check(winner.stop_eof() == 0, "the promoted primary exited 0")


def scenario_concurrent():
    """3 concurrent mcp-servers plus one ACP: one primary, and every write lands."""
    setup_project()
    a_log = os.path.join(S, "a-acp.log")

    acp, acp_ok = start_acp("a-acp", a_log)
    mcps = []
    for idx in range(3):
        proc, ok = start_mcp("mcp%d" % idx)
        mcps.append((proc, ok))
    check(acp_ok, "the ACP instance answered initialize")
    check(all(ok for _, ok in mcps), "all 3 mcp-servers answered MCP initialize")
    check(wait_for_db(90), "the project database exists")

    entries = wait_for_registry(lambda e: len(e) == 4, 60)
    check(len(entries) == 4, "the registry holds all 4 instances: %s" % [e.get("mode") for e in entries])
    primaries = [e for e in entries if e.get("is_primary")]
    check(len(primaries) == 1, "exactly one instance is primary: %s" % [(e.get("mode"), e.get("is_primary")) for e in entries])
    pids = {e.get("pid") for e in entries}
    check((lock_info() or {}).get("pid") in pids, "the lock file names one of the running instances")
    check(sum(1 for e in entries if e.get("mode") == "mcp") == 3, "3 instances registered as mode=mcp")

    # Exactly one process ran the primary-only services across every log.
    all_logs = [CLILOG, a_log]
    starts = sum(count(path, PRIMARY_START) for path in all_logs)
    check(starts == 1, "exactly one process started the primary-only background services (found %d)" % starts)

    # Every instance writes: each mcp-server deletes its own seeded key, and
    # the ACP instance creates a session.
    keys = []
    for idx in range(3):
        key = "p6.concurrent.%d" % idx
        seed_memory(key)
        keys.append(key)
    check(all(memory_rows(key) == 1 for key in keys), "seeded one memory row per mcp-server")

    sessions_before = scalar("SELECT COUNT(*) FROM sessions") or 0
    results = {}

    def do_forget(index, proc, key):
        proc.send(tool_call(70 + index, "forget", {"key": key}))
        results[index] = proc.wait_id(70 + index, timeout=120)

    threads = []
    for idx, ((proc, _), key) in enumerate(zip(mcps, keys)):
        thread = threading.Thread(target=do_forget, args=(idx, proc, key))
        thread.start()
        threads.append(thread)
    acp.send(acp_session_new(80))
    acp_resp = acp.wait_id(80, timeout=120)
    for thread in threads:
        thread.join(150)

    for idx, key in enumerate(keys):
        check(not tool_failed(results.get(idx)),
              "mcp-server %d's write succeeded: %s" % (idx, json.dumps(results.get(idx))[:160]))
        check(memory_rows(key) == 0, "mcp-server %d's write landed in the database" % idx)
    check(acp_resp is not None and "error" not in acp_resp, "the ACP instance's session/new succeeded")
    check((scalar("SELECT COUNT(*) FROM sessions") or 0) > sessions_before,
          "the ACP instance's write landed in the database")

    for idx, (proc, _) in enumerate(mcps):
        check(proc.stop_eof() == 0, "mcp-server %d exited 0" % idx)
    check(acp.stop_eof() == 0, "the ACP instance exited 0")


def scenario_cronjob():
    """`cronjob run` is a one-shot; a cron edit on a secondary reaches the primary."""
    setup_project()
    a_log = os.path.join(S, "a-acp.log")

    acp, acp_ok = start_acp("a-acp", a_log)
    check(acp_ok, "the ACP primary answered initialize")
    check(wait_for(a_log, PRIMARY_START, 60), "the ACP instance is the primary")
    check((lock_info() or {}).get("pid") == acp.pid, "the lock names the ACP primary")

    # --- one-shot `cronjob run` next to the primary ---
    before_promotions = count(a_log, PROMOTED)
    proc, elapsed = run_cli("cronjob-run", ["cronjob", "run", "p6job"], timeout=180)
    check(proc.returncode == 0, "`cronjob run` exited 0 (stderr tail: %r)"
          % proc.stderr.decode(errors="replace").strip()[-200:])
    check(elapsed < 60, "`cronjob run` exited on its own in %.2fs" % elapsed)
    oneshot_line = line_with(CLILOG, ONESHOT)
    check("role=secondary" in oneshot_line and "startup_mode=cronjob" in oneshot_line,
          "`cronjob run` ran as a one-shot secondary: " + oneshot_line)
    check(count(CLILOG, ONESHOT_NO_PROMOTE) >= 1, "`cronjob run` never armed failover promotion")
    check(count(CLILOG, PRIMARY_START) == 0, "`cronjob run` started no primary-only services")
    check(count(CLILOG, CRON_SCHEDULED) == 0, "`cronjob run` scheduled no cron job of its own")
    check(count(a_log, PROMOTED) == before_promotions, "nobody was promoted while `cronjob run` ran")
    check(acp.p.poll() is None and (lock_info() or {}).get("pid") == acp.pid,
          "the ACP primary is untouched and still holds the lock")

    # --- a cron edit through a `serve` secondary reaches the primary ---
    serve = Proc("serve", ["serve", "--host", "127.0.0.1", "--port", "18796"])
    check(wait_for(CLILOG, SECONDARY_SKIP, 90), "`pando serve` joined as a secondary")
    port = None
    deadline = time.time() + 60
    while time.time() < deadline and port is None:
        with serve._lock:
            lines = list(serve.lines)
        for line in lines:
            if "listening on" in line:
                try:
                    port = int(line.rsplit(":", 1)[1])
                except ValueError:
                    pass
        time.sleep(0.2)
    port = port or 18796

    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    token = None
    try:
        with urllib.request.urlopen(
            "https://127.0.0.1:%d/api/v1/token" % port, context=ctx, timeout=30
        ) as resp:
            doc = json.loads(resp.read() or b"{}")
        token = doc.get("token") if isinstance(doc, dict) else None
    except (urllib.error.URLError, ValueError, OSError) as exc:
        check(False, "could not fetch the serve API token: %s" % exc)
    check(bool(token), "the serve secondary handed out its API token")

    status = None
    if token:
        body = json.dumps({"name": "p6new", "schedule": "15 5 * * *", "prompt": "noop",
                           "enabled": True, "engine": "p6-nonexistent-engine"}).encode()
        req = urllib.request.Request(
            "https://127.0.0.1:%d/api/v1/cronjobs" % port, data=body,
            headers={"Content-Type": "application/json", "X-Pando-Token": token},
            method="POST")
        started = time.time()
        try:
            with urllib.request.urlopen(req, context=ctx, timeout=60) as resp:
                status = resp.status
        except urllib.error.HTTPError as exc:
            status = exc.code
            print("POST body:", exc.read()[:300], flush=True)
        check(status == 201, "POST /api/v1/cronjobs on the secondary returned %s" % status)
        check(wait_for(a_log, RELOAD_APPLIED, 30),
              "the primary applied the edit over cronjob.reload: " + line_with(a_log, RELOAD_APPLIED))
        print("NOTE the cron edit reached the primary in %.3fs" % (time.time() - started), flush=True)
        scheduled = [l for l in text(a_log).splitlines() if CRON_SCHEDULED in l and "name=p6new" in l]
        check(len(scheduled) == 1, "the primary scheduled p6new exactly once: %s" % (scheduled[0].strip() if scheduled else "<none>"))
        check(count(CLILOG, RELOAD_SENT) >= 1, "the secondary handed the edit to the primary")
        check("p6new" in text(os.path.join(PROJ, ".pando.toml")), "the edit was persisted to .pando.toml")

    check(serve.stop_signal(signal.SIGTERM, timeout=60) == 0, "`pando serve` exited 0 after SIGTERM")
    check(acp.stop_eof() == 0, "the ACP primary exited 0")


def scenario_handover_write():
    """A KB write issued while the primary hands over must land on the new primary."""
    setup_project()
    a_log = os.path.join(S, "a-acp.log")
    b_log = os.path.join(S, "b-acp.log")

    a, a_ok = start_acp("a-acp", a_log)
    check(a_ok, "the ACP primary answered initialize")
    check(wait_for(a_log, PRIMARY_START, 60), "the ACP instance is the primary")
    check(wait_for_db(60), "the project database exists")

    b, b_ok = start_acp("b-acp", b_log)
    writer, writer_ok = start_mcp("c-mcp")
    check(b_ok and writer_ok, "the ACP secondary and the mcp-server secondary both answered initialize")
    check(wait_for(b_log, SECONDARY_SKIP, 60), "the ACP secondary is a promotion candidate")

    key = "p6.handover"
    seed_memory(key)
    check(memory_rows(key) == 1, "seeded the memory row the handover write will delete")

    result = {}

    def write_during_handover():
        writer.send(tool_call(90, "forget", {"key": key}))
        result["resp"] = writer.wait_id(90, timeout=180)

    thread = threading.Thread(target=write_during_handover)
    thread.start()
    # Issue the write and tear the primary down at essentially the same moment,
    # so the forward lands inside the drain -> release -> promote window.
    time.sleep(0.05)
    check(a.stop_eof(timeout=90) == 0, "the primary exited 0 after stdin EOF")
    thread.join(240)

    check(wait_for(b_log, PROMOTED, 60), "the ACP secondary was promoted")
    check(not tool_failed(result.get("resp")),
          "the write issued during the handover succeeded: %s" % json.dumps(result.get("resp"))[:300])
    check(memory_rows(key) == 0, "the handover write was applied by the new primary")

    check(writer.stop_eof() == 0, "the mcp-server secondary exited 0")
    check(b.stop_eof() == 0, "the promoted primary exited 0")


SCENARIOS = {
    "join": scenario_join,
    "stop_eof": lambda: scenario_stop("eof"),
    "stop_term": lambda: scenario_stop("term"),
    "stop_kill": lambda: scenario_stop("kill"),
    "concurrent": scenario_concurrent,
    "cronjob": scenario_cronjob,
    "handover_write": scenario_handover_write,
}


def run_child(name):
    if not BIN or not S:
        print("BIN and S must be set by the parent", file=sys.stderr)
        sys.exit(2)
    scenario = SCENARIOS.get(name)
    if scenario is None:
        print("unknown scenario %r" % name, file=sys.stderr)
        sys.exit(2)
    try:
        scenario()
    finally:
        # Never leave a Pando process behind, whatever happened above.
        leftover = subprocess.run(["pgrep", "-f", BIN], capture_output=True, text=True).stdout.split()
        for pid in leftover:
            try:
                os.kill(int(pid), signal.SIGKILL)
            except (ValueError, ProcessLookupError, PermissionError):
                pass
        check(not leftover, "no stray pando processes were left running: %r" % leftover)
    finish()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--scenario", help="run one scenario (used inside the sandbox)")
    args, remaining = parser.parse_known_args()
    if args.scenario:
        run_child(args.scenario)
    else:
        unittest.main(argv=[sys.argv[0]] + remaining)
