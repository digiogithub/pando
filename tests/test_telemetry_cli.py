"""
End-to-end tests for `pando telemetry` (opt-in remote diagnostics shipped to
a Better Stack-compatible ingest endpoint).

These tests build the real `pando` binary and drive it via subprocess, the
same way tests/test_cronjob_cli.py and tests/test_desktop_controller_mcp_e2e.py
drive their CLI/MCP surfaces. No test here ever talks to the real Better Stack
service: a local Python http.server-based mock stands in for the ingest
endpoint via PANDO_TELEMETRY_ENDPOINT, and PANDO_TELEMETRY_TOKEN=test is used
as the (fake) ingest token. $HOME/$XDG_CONFIG_HOME are always pointed at a
throwaway temp directory so nothing here ever reads or writes the real
~/.pando.toml on the machine running the tests.

Covers:
- `pando telemetry status` with no token available: available=no, enabled=false.
- With PANDO_TELEMETRY_TOKEN=test: `enable` prints and persists a 16-digit debug
  id (grouped for display, 19 chars); `id` prints the same value; the change is
  written to the isolated *global* config file, never into the project's own
  working directory; `disable` keeps the id; re-`enable` reuses it; `regenerate`
  replaces it.
- Shipping: with telemetry enabled and PANDO_TELEMETRY_ENDPOINT pointed at a
  local mock server, running `pando serve` for a few seconds and then sending
  SIGINT/SIGTERM ships at least one batch of JSON records carrying the right
  Authorization header, the configured debug_id (grouped or raw — see note
  below), app.version, and a "Telemetry enabled"/shutdown lifecycle message —
  with no leaked token string and no leaked absolute $HOME path (must read as
  "~" instead).
- With telemetry left disabled, the same `pando serve` run ships nothing.

Run with:
    python3 -m pytest tests/test_telemetry_cli.py -v
    python3 -m unittest tests/test_telemetry_cli.py

Skips gracefully (does not fail) when the `go` toolchain is not on PATH.
"""

import json
import os
import shutil
import signal
import socket
import subprocess
import tempfile
import threading
import time
import unittest
from http.server import BaseHTTPRequestHandler, HTTPServer

PANDO_ROOT = "/www/MCP/Pando/pando"
PANDO_BIN = os.path.join(PANDO_ROOT, "pando-telemetry-e2e-bin")

# Keep the whole module comfortably under the 60s budget: two `pando serve`
# runs (shipping + disabled) dominate the wall-clock time.
SERVE_STARTUP_TIMEOUT_SECONDS = 12
SERVE_SHUTDOWN_TIMEOUT_SECONDS = 12
CLI_TIMEOUT_SECONDS = 15

GO_AVAILABLE = shutil.which("go") is not None


def setUpModule():
    """Build the pando binary once before all tests in this module. Skips the
    whole module gracefully when no Go toolchain is available, per the task's
    "skip gracefully" requirement (unlike this repo's other *_cli.py /
    *_e2e.py fixtures, which hard-fail on a missing toolchain)."""
    if not GO_AVAILABLE:
        raise unittest.SkipTest("go toolchain not found on PATH; skipping telemetry E2E tests")

    # No -ldflags: an intentionally token-less build, exactly like a plain
    # `go build`/`go install`/fork would produce. telemetry.Available() must
    # come from PANDO_TELEMETRY_TOKEN alone in every test below.
    result = subprocess.run(
        ["go", "build", "-o", PANDO_BIN, "."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(
            "Failed to build pando binary for telemetry e2e tests:\n"
            + result.stdout
            + result.stderr
        )


def tearDownModule():
    if os.path.exists(PANDO_BIN):
        os.remove(PANDO_BIN)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _free_port() -> int:
    """Grabs an OS-assigned ephemeral TCP port on loopback and releases it
    immediately -- good enough for handing to a subprocess a moment later."""
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _isolated_env(home_dir: str, **extra) -> dict:
    """Builds a subprocess environment isolated to home_dir: $HOME and
    $XDG_CONFIG_HOME point there, so config.UpdateTelemetry's GLOBAL-only
    writer (and any AGE key material config.Load might touch) never reaches
    the real developer machine's ~/.pando.toml or ~/.config/pando. Any
    telemetry-related variable already in this test process's own environment
    is stripped first so it can never leak into the child by accident."""
    env = dict(os.environ)
    env["HOME"] = home_dir
    env["XDG_CONFIG_HOME"] = ""
    for key in ("PANDO_TELEMETRY_TOKEN", "PANDO_TELEMETRY_ENDPOINT", "PANDO_BETTERSTACK_TOKEN"):
        env.pop(key, None)
    env.update(extra)
    return env


def _run_pando(*args, cwd: str, env: dict) -> subprocess.CompletedProcess:
    return subprocess.run(
        [PANDO_BIN, *args],
        cwd=cwd,
        env=env,
        capture_output=True,
        text=True,
        timeout=CLI_TIMEOUT_SECONDS,
    )


class _MockIngestHandler(BaseHTTPRequestHandler):
    """Captures every POST this Better Stack-shaped mock ingest endpoint
    receives: the Authorization header and the raw (still-encoded) body, plus
    the JSON-decoded body when it parses as one. Shared state lives on the
    server instance (see _MockIngestServer) so multiple requests accumulate."""

    def do_POST(self):  # noqa: N802 (BaseHTTPRequestHandler's naming convention)
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b""
        try:
            decoded = json.loads(raw.decode("utf-8"))
        except (ValueError, UnicodeDecodeError):
            decoded = None
        with self.server.lock:
            self.server.requests.append(
                {
                    "authorization": self.headers.get("Authorization", ""),
                    "raw_body": raw.decode("utf-8", errors="replace"),
                    "json_body": decoded,
                }
            )
        self.send_response(202)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def log_message(self, format, *args):  # noqa: A002 - silence default stderr logging
        pass


class _MockIngestServer:
    """A tiny background HTTP server standing in for Better Stack's ingest
    endpoint, per the hard constraint that no test ever talks to the real
    service."""

    def __init__(self):
        self._httpd = HTTPServer(("127.0.0.1", 0), _MockIngestHandler)
        self._httpd.lock = threading.Lock()
        self._httpd.requests = []
        self._thread = threading.Thread(target=self._httpd.serve_forever, daemon=True)

    @property
    def url(self) -> str:
        host, port = self._httpd.server_address
        return f"http://{host}:{port}"

    @property
    def requests(self) -> list:
        with self._httpd.lock:
            return list(self._httpd.requests)

    def start(self):
        self._thread.start()

    def stop(self):
        self._httpd.shutdown()
        self._thread.join(timeout=5)
        self._httpd.server_close()


def _all_records(mock: _MockIngestServer) -> list:
    """Flattens every JSON-array body the mock received into one list of
    record dicts (records are always shipped as a JSON array, per the
    protocol described in the telemetry plan)."""
    records = []
    for req in mock.requests:
        body = req["json_body"]
        if isinstance(body, list):
            records.extend(body)
        elif isinstance(body, dict):
            records.append(body)
    return records


def _run_serve_and_capture(
    home_dir: str, project_dir: str, mock: _MockIngestServer, *, extra_env: dict
) -> subprocess.CompletedProcess:
    """Starts `pando serve` (isolated HOME, pointed at the mock ingest
    endpoint), waits for it to report it is listening (or a startup timeout),
    lets it run briefly so telemetry has a chance to initialize, then sends a
    graceful shutdown signal and waits for exit. Returns the finished
    subprocess.CompletedProcess-like result (returncode + captured output)."""
    port = _free_port()
    env = _isolated_env(home_dir, PANDO_TELEMETRY_ENDPOINT=mock.url, **extra_env)

    proc = subprocess.Popen(
        [PANDO_BIN, "serve", "--host", "127.0.0.1", "--port", str(port)],
        cwd=project_dir,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
    )

    output_lines = []
    deadline = time.time() + SERVE_STARTUP_TIMEOUT_SECONDS
    saw_listening = False
    try:
        while time.time() < deadline:
            line = proc.stdout.readline()
            if line:
                output_lines.append(line)
                if "listening on" in line.lower():
                    saw_listening = True
                    break
            elif proc.poll() is not None:
                break  # the process exited before it ever reported listening
    finally:
        pass

    # Give the app a moment past "listening" for its own async init (telemetry
    # start, IPC bus, etc.) to actually run and log its lifecycle record,
    # whether or not we saw the "listening" line in time.
    time.sleep(1.5 if saw_listening else 0.5)

    proc.send_signal(signal.SIGINT)
    try:
        proc.wait(timeout=SERVE_SHUTDOWN_TIMEOUT_SECONDS)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)

    # Drain any remaining buffered output, then release the pipe.
    try:
        remaining = proc.stdout.read()
        if remaining:
            output_lines.append(remaining)
    except Exception:
        pass
    finally:
        proc.stdout.close()

    # The mock's background thread may still be a beat behind the client's
    # final flush landing on the wire; give it a short grace window.
    time.sleep(0.5)

    return proc.returncode, "".join(output_lines)


def _write_minimal_project(project_dir: str) -> None:
    os.makedirs(os.path.join(project_dir, ".pando", "data"), exist_ok=True)


# ---------------------------------------------------------------------------
# a. status with no token
# ---------------------------------------------------------------------------


@unittest.skipUnless(GO_AVAILABLE, "go toolchain not found on PATH")
class TestTelemetryStatusUnavailable(unittest.TestCase):
    def test_status_json_reports_unavailable_and_disabled(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            env = _isolated_env(home_dir)  # no PANDO_TELEMETRY_TOKEN
            result = _run_pando("telemetry", "status", "--json", cwd=project_dir, env=env)

        self.assertEqual(result.returncode, 0, msg=result.stderr)
        status = json.loads(result.stdout)
        self.assertFalse(status["available"], "available must be false with no ingest token")
        self.assertFalse(status["enabled"], "enabled must default to false")
        self.assertEqual(status.get("debugId", ""), "")

    def test_status_text_output_mentions_unavailable(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            env = _isolated_env(home_dir)
            result = _run_pando("telemetry", cwd=project_dir, env=env)  # default action = status

        self.assertEqual(result.returncode, 0, msg=result.stderr)
        self.assertIn("available:  no", result.stdout)
        self.assertIn("enabled:    no", result.stdout)


# ---------------------------------------------------------------------------
# b. enable / id / disable / regenerate lifecycle
# ---------------------------------------------------------------------------


@unittest.skipUnless(GO_AVAILABLE, "go toolchain not found on PATH")
class TestTelemetryEnableLifecycle(unittest.TestCase):
    def test_enable_id_disable_regenerate_flow(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            env = _isolated_env(home_dir, PANDO_TELEMETRY_TOKEN="test")

            enable = _run_pando("telemetry", "enable", cwd=project_dir, env=env)
            self.assertEqual(enable.returncode, 0, msg=enable.stderr)

            grouped_ids = [
                tok
                for tok in enable.stdout.replace("\n", " ").split()
                if len(tok) == 19 and tok.count("-") == 3
            ]
            self.assertTrue(grouped_ids, f"enable output has no 19-char grouped id:\n{enable.stdout}")
            first_id = grouped_ids[-1]
            self.assertEqual(len(first_id), 19)

            # `id` prints exactly the same grouped id, alone, script-friendly.
            id_result = _run_pando("telemetry", "id", cwd=project_dir, env=env)
            self.assertEqual(id_result.returncode, 0, msg=id_result.stderr)
            self.assertEqual(id_result.stdout.strip(), first_id)

            # The write landed in the isolated GLOBAL config dir, never in the
            # project's own working directory.
            self.assertTrue(
                any(f.startswith(".pando.") for f in os.listdir(home_dir)),
                f"no global config file was written under the isolated HOME ({home_dir}): {os.listdir(home_dir)}",
            )
            project_pando_files = [f for f in os.listdir(project_dir) if f.startswith(".pando.")]
            self.assertEqual(
                project_pando_files, [],
                f"telemetry enable must never write a project-local config file, found: {project_pando_files}",
            )

            # disable keeps the id.
            disable = _run_pando("telemetry", "disable", cwd=project_dir, env=env)
            self.assertEqual(disable.returncode, 0, msg=disable.stderr)
            status_after_disable = json.loads(
                _run_pando("telemetry", "status", "--json", cwd=project_dir, env=env).stdout
            )
            self.assertFalse(status_after_disable["enabled"])
            self.assertEqual(status_after_disable["debugId"], first_id)

            # re-enable reuses the same id.
            reenable = _run_pando("telemetry", "enable", cwd=project_dir, env=env)
            self.assertEqual(reenable.returncode, 0, msg=reenable.stderr)
            self.assertIn(first_id, reenable.stdout)

            # regenerate replaces it.
            regenerate = _run_pando("telemetry", "regenerate", cwd=project_dir, env=env)
            self.assertEqual(regenerate.returncode, 0, msg=regenerate.stderr)
            status_after_regen = json.loads(
                _run_pando("telemetry", "status", "--json", cwd=project_dir, env=env).stdout
            )
            new_id = status_after_regen["debugId"]
            self.assertEqual(len(new_id), 19)
            self.assertNotEqual(new_id, first_id, "regenerate must replace the debug id")

    def test_enable_refused_without_token(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            env = _isolated_env(home_dir)  # no token: unavailable in this build
            result = _run_pando("telemetry", "enable", cwd=project_dir, env=env)

        self.assertNotEqual(result.returncode, 0, "enable without a token must fail (non-zero exit)")
        self.assertIn("not available", (result.stdout + result.stderr).lower())

    def test_id_fails_without_one(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            env = _isolated_env(home_dir)
            result = _run_pando("telemetry", "id", cwd=project_dir, env=env)

        self.assertNotEqual(result.returncode, 0, "id must fail (non-zero exit) when no debug id exists yet")
        self.assertEqual(result.stdout.strip(), "", "id must print nothing to stdout on failure")


# ---------------------------------------------------------------------------
# c/d. real shipping over the wire, via `pando serve`
# ---------------------------------------------------------------------------


@unittest.skipUnless(GO_AVAILABLE, "go toolchain not found on PATH")
class TestTelemetryShipping(unittest.TestCase):
    def test_enabled_telemetry_ships_records_to_mock_ingest(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            _write_minimal_project(project_dir)
            enable_env = _isolated_env(home_dir, PANDO_TELEMETRY_TOKEN="test")
            enable = _run_pando("telemetry", "enable", cwd=project_dir, env=enable_env)
            self.assertEqual(enable.returncode, 0, msg=enable.stderr)
            status = json.loads(
                _run_pando("telemetry", "status", "--json", cwd=project_dir, env=enable_env).stdout
            )
            debug_id_grouped = status["debugId"]
            debug_id_raw = debug_id_grouped.replace("-", "")
            self.assertEqual(len(debug_id_raw), 16)

            mock = _MockIngestServer()
            mock.start()
            try:
                returncode, output = _run_serve_and_capture(
                    home_dir, project_dir, mock, extra_env={"PANDO_TELEMETRY_TOKEN": "test"}
                )
                requests = mock.requests
            finally:
                mock.stop()

        self.assertTrue(requests, f"mock ingest endpoint received no requests at all.\nserve output:\n{output}")

        for req in requests:
            self.assertEqual(
                req["authorization"], "Bearer test",
                f"Authorization header = {req['authorization']!r}, want 'Bearer test'",
            )
            self.assertIsInstance(
                req["json_body"], list,
                f"request body is not a JSON array: {req['raw_body']!r}",
            )

        records = _all_records(mock)
        self.assertTrue(records, "no individual records were found inside the shipped batches")

        for record in records:
            self.assertIn("dt", record, f"record missing 'dt': {record}")

        # debug_id: accept either the grouped display form (current behavior)
        # or the raw 16-digit form, since which one records carry is owned by
        # internal/telemetry and may change independently of this test.
        seen_debug_ids = {r.get("debug_id") for r in records if r.get("debug_id")}
        self.assertTrue(seen_debug_ids, f"no record carried a debug_id: {records}")
        for seen in seen_debug_ids:
            seen_raw = seen.replace("-", "")
            self.assertEqual(
                seen_raw, debug_id_raw,
                f"record debug_id {seen!r} does not match the enabled id {debug_id_grouped!r}",
            )

        app_versions = {r.get("app", {}).get("version") for r in records if isinstance(r.get("app"), dict)}
        app_versions.discard(None)
        app_versions.discard("")
        self.assertTrue(app_versions, f"no record carried a non-empty app.version: {records}")

        # A lifecycle message: telemetry starting up and/or the app shutting
        # down. Match loosely (message text or an "event" attr) since the
        # exact wording is owned by internal/app/internal/logging.
        def _mentions_lifecycle(rec: dict) -> bool:
            msg = str(rec.get("message", "")).lower()
            attrs = rec.get("attrs") or {}
            event = str(attrs.get("event", "")).lower() if isinstance(attrs, dict) else ""
            return (
                "telemetry enabled" in msg
                or "shutdown" in msg
                or "shutdown" in event
                or "telemetry" in event
            )

        self.assertTrue(
            any(_mentions_lifecycle(r) for r in records),
            f"no record mentions a telemetry/shutdown lifecycle event: {records}",
        )

        # No leaked token, and no leaked absolute isolated-$HOME path (must
        # have been rewritten to "~").
        for req in requests:
            self.assertNotIn(
                "test", req["raw_body"],
                f"the literal ingest token leaked into a shipped record body: {req['raw_body']!r}",
            )
            self.assertNotIn(
                home_dir, req["raw_body"],
                f"the isolated $HOME absolute path leaked into a shipped record body "
                f"(should have been rewritten to '~'): {req['raw_body']!r}",
            )

    def test_disabled_telemetry_ships_nothing(self):
        with tempfile.TemporaryDirectory() as home_dir, tempfile.TemporaryDirectory() as project_dir:
            _write_minimal_project(project_dir)
            # Telemetry is never enabled here (default state). A token is
            # still supplied so the build considers it *available* -- the
            # point of this test is that "available but disabled" ships
            # nothing, not that an unavailable build ships nothing (that is
            # covered implicitly by every other build never shipping either).
            mock = _MockIngestServer()
            mock.start()
            try:
                returncode, output = _run_serve_and_capture(
                    home_dir, project_dir, mock, extra_env={"PANDO_TELEMETRY_TOKEN": "test"}
                )
                requests = mock.requests
            finally:
                mock.stop()

        self.assertEqual(
            requests, [],
            f"disabled telemetry must ship nothing, but the mock ingest endpoint received "
            f"{len(requests)} request(s).\nserve output:\n{output}",
        )


if __name__ == "__main__":
    unittest.main()
