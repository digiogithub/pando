"""
Black-box tests for the host command sandbox (PANDO-EP-0009) through the real
`pando` binary: `pando sandbox status --json`, `pando sandbox exec -- ...` and
the settings API (`GET/PUT /api/v1/config/sandbox` on `pando serve`).

The binary is built once per module (like tests/test_telemetry_cli.py). $HOME
and $XDG_CONFIG_HOME always point at a throwaway directory, and PANDO_SANDBOX
is stripped from the environment unless a test sets it, so nothing here reads
or writes the developer's real ~/.pando.toml.

Where the files live matters: in every mode the temp dirs (/tmp, /var/tmp,
$TMPDIR) are writable by design, so the isolated HOME, the workspace and the
"outside" directory (standing in for any path outside the workspace) all live
in a scratch directory created next to this file, inside the repository
checkout (ignored by .gitignore), which no writable root covers.

Enforcement-dependent assertions skip (not fail) when `status` reports the
backend is not enforced here (Windows, Linux without Landlock, ...).

Run with:
    python3 -m pytest tests/test_sandbox_cli.py -v
"""

import json
import os
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request

TESTS_DIR = os.path.dirname(os.path.abspath(__file__))
PANDO_ROOT = os.path.dirname(TESTS_DIR)

CLI_TIMEOUT_SECONDS = 30
SERVE_STARTUP_TIMEOUT_SECONDS = 30

GO_AVAILABLE = shutil.which("go") is not None

_build_dir = None
PANDO_BIN = None


def setUpModule():
    global _build_dir, PANDO_BIN
    if not GO_AVAILABLE:
        raise unittest.SkipTest("go toolchain not found on PATH; skipping sandbox CLI tests")
    _build_dir = tempfile.mkdtemp(prefix="pando-sandbox-bin-")
    PANDO_BIN = os.path.join(_build_dir, "pando")
    result = subprocess.run(
        ["go", "build", "-o", PANDO_BIN, "."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError("Failed to build pando for sandbox tests:\n" + result.stdout + result.stderr)


def tearDownModule():
    if _build_dir:
        shutil.rmtree(_build_dir, ignore_errors=True)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


class SandboxEnv:
    """An isolated HOME, a workspace with a `src/` subdirectory and a project
    config, and an "outside" directory. All three live in a scratch dir next
    to this file: under /tmp they would be writable by design (temp dirs are
    writable roots in every mode, read-only included)."""

    def __init__(self):
        self.base = os.path.realpath(tempfile.mkdtemp(prefix=".sandbox-e2e-", dir=TESTS_DIR))
        self.home = os.path.join(self.base, "home")
        self.ws = os.path.join(self.base, "ws")
        self.outside = os.path.join(self.base, "outside")
        for d in (self.home, self.outside, os.path.join(self.ws, "src")):
            os.makedirs(d)
        # A pre-existing subdirectory: the Landlock-only backend (no
        # bubblewrap) denies new top-level entries in the workspace root.
        with open(os.path.join(self.ws, ".pando.toml"), "w") as f:
            f.write("# project config\n")

    def cleanup(self):
        shutil.rmtree(self.base, ignore_errors=True)

    def env(self, **extra):
        env = dict(os.environ)
        env["HOME"] = self.home
        env["XDG_CONFIG_HOME"] = ""
        env["LC_ALL"] = "C"
        env.pop("PANDO_SANDBOX", None)
        env.update(extra)
        return env

    def run(self, *args, **extra_env):
        return subprocess.run(
            [PANDO_BIN, *args],
            cwd=self.ws,
            env=self.env(**extra_env),
            capture_output=True,
            text=True,
            timeout=CLI_TIMEOUT_SECONDS,
        )

    def status(self, **extra_env):
        res = self.run("sandbox", "status", "--json", **extra_env)
        if res.returncode != 0:
            raise AssertionError(f"sandbox status failed ({res.returncode}):\n{res.stdout}{res.stderr}")
        # Log lines go to stderr; stdout is the JSON document.
        return json.loads(res.stdout)

    def exec_sh(self, script, **extra_env):
        return self.run("sandbox", "exec", "--", "sh", "-c", script, **extra_env)


class _Base(unittest.TestCase):
    def setUp(self):
        self.sbx = SandboxEnv()
        self.addCleanup(self.sbx.cleanup)

    def require_enforced(self):
        st = self.sbx.status()
        if not st["enforced"]:
            self.skipTest(f"sandbox backend {st['backend']!r} not enforced here: {st.get('reason', '')}")
        return st


# ---------------------------------------------------------------------------
# pando sandbox status
# ---------------------------------------------------------------------------


@unittest.skipUnless(GO_AVAILABLE, "go toolchain not found on PATH")
class TestSandboxStatus(_Base):
    def test_status_json_defaults(self):
        st = self.sbx.status()
        for key in ("backend", "enforced", "active", "mode", "network", "source", "workspace", "policyHash", "autoAllowBash"):
            self.assertIn(key, st)
        self.assertEqual(st["mode"], "workspace-write", st)
        self.assertEqual(st["source"], "default", st)
        self.assertEqual(st["network"], "allowed", st)
        self.assertEqual(os.path.realpath(st["workspace"]), self.sbx.ws)
        self.assertIn(self.sbx.ws, st["writableRoots"])
        protected = st.get("protectedPaths", [])
        for rel in (".pando.toml", ".pando", os.path.join(".git", "hooks"), os.path.join(".git", "config")):
            self.assertIn(os.path.join(self.sbx.ws, rel), protected)
        self.assertEqual(st["active"], st["enforced"], "default policy is enabled, so active == enforced")
        self.assertEqual(len(st["policyHash"]), 64)

    def test_env_override_off(self):
        st = self.sbx.status(PANDO_SANDBOX="off")
        self.assertEqual(st["mode"], "off")
        self.assertEqual(st["source"], "env")
        self.assertFalse(st["active"])

    def test_env_override_read_only_restricts_network(self):
        st = self.sbx.status(PANDO_SANDBOX="read-only")
        self.assertEqual(st["mode"], "read-only")
        self.assertEqual(st["network"], "restricted")
        self.assertNotIn(self.sbx.ws, st.get("writableRoots", []))

    def test_project_config_cannot_loosen(self):
        # A project config may only tighten: Disabled=true there is ignored.
        with open(os.path.join(self.sbx.ws, ".pando.toml"), "w") as f:
            f.write("[Sandbox]\nDisabled = true\n")
        st = self.sbx.status()
        self.assertEqual(st["mode"], "workspace-write", st)

    def test_global_config_disables(self):
        with open(os.path.join(self.sbx.home, ".pando.toml"), "w") as f:
            f.write("[Sandbox]\nDisabled = true\n")
        st = self.sbx.status()
        self.assertEqual(st["mode"], "off", st)
        self.assertEqual(st["source"], "config", st)


# ---------------------------------------------------------------------------
# pando sandbox exec
# ---------------------------------------------------------------------------


@unittest.skipUnless(GO_AVAILABLE, "go toolchain not found on PATH")
class TestSandboxExec(_Base):
    def test_workspace_write_allowed(self):
        res = self.sbx.exec_sh("echo hi > src/inside.txt")
        self.assertEqual(res.returncode, 0, res.stderr)
        self.assertTrue(os.path.exists(os.path.join(self.sbx.ws, "src", "inside.txt")))

    def test_temp_write_allowed(self):
        target = os.path.join(tempfile.gettempdir(), f"pando-sandbox-tmp-{os.getpid()}")
        self.addCleanup(lambda: os.path.exists(target) and os.remove(target))
        res = self.sbx.exec_sh(f"echo hi > '{target}'")
        self.assertEqual(res.returncode, 0, res.stderr)

    def test_outside_write_denied(self):
        self.require_enforced()
        target = os.path.join(self.sbx.outside, "x.txt")
        res = self.sbx.exec_sh(f"echo hi > '{target}'")
        self.assertNotEqual(res.returncode, 0, "write outside the workspace succeeded under the sandbox")
        self.assertFalse(os.path.exists(target))
        self.assertIn("running confined", res.stderr)

    def test_protected_project_config_denied(self):
        self.require_enforced()
        # Pando itself may rewrite the file while loading config; compare
        # with what is there right before the confined command runs.
        path = os.path.join(self.sbx.ws, ".pando.toml")
        with open(path) as f:
            before = f.read()
        res = self.sbx.exec_sh("echo tampered > .pando.toml")
        self.assertNotEqual(res.returncode, 0)
        with open(path) as f:
            self.assertEqual(f.read(), before)

    def test_protected_global_config_denied(self):
        self.require_enforced()
        global_cfg = os.path.join(self.sbx.home, ".pando.toml")
        with open(global_cfg, "w") as f:
            f.write("# global config\n")
        res = self.sbx.exec_sh(f"echo '[Sandbox]' > '{global_cfg}'")
        self.assertNotEqual(res.returncode, 0)
        with open(global_cfg) as f:
            self.assertEqual(f.read(), "# global config\n")
        # Creating a new file directly in $HOME is denied too.
        res = self.sbx.exec_sh(f"echo '{{}}' > '{self.sbx.home}/.pando.json'")
        self.assertNotEqual(res.returncode, 0)

    def test_read_only_mode_denies_workspace_write(self):
        self.require_enforced()
        res = self.sbx.exec_sh("echo hi > src/ro.txt", PANDO_SANDBOX="read-only")
        self.assertNotEqual(res.returncode, 0)
        self.assertFalse(os.path.exists(os.path.join(self.sbx.ws, "src", "ro.txt")))

    def test_env_off_allows_outside_write(self):
        target = os.path.join(self.sbx.outside, "off.txt")
        res = self.sbx.exec_sh(f"echo hi > '{target}'", PANDO_SANDBOX="off")
        self.assertEqual(res.returncode, 0, res.stderr)
        self.assertTrue(os.path.exists(target))
        self.assertIn("policy is off", res.stderr)

    def test_exit_code_propagates(self):
        res = self.sbx.exec_sh("exit 7")
        self.assertEqual(res.returncode, 7)

    def test_secrets_scrubbed(self):
        res = self.sbx.exec_sh('echo "key=${OPENAI_API_KEY:-unset}"', OPENAI_API_KEY="sk-test-should-not-leak")
        self.assertEqual(res.returncode, 0, res.stderr)
        self.assertIn("key=unset", res.stdout)


# ---------------------------------------------------------------------------
# Settings API on `pando serve`
# ---------------------------------------------------------------------------


def _free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def _http(method, url, token=None, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if token:
        req.add_header("X-Pando-Token", token)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        # `pando serve` uses a self-signed certificate on loopback.
        ctx = ssl._create_unverified_context()
        with urllib.request.urlopen(req, timeout=10, context=ctx) as resp:
            return resp.status, json.loads(resp.read() or b"null")
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, raw.decode(errors="replace")


@unittest.skipUnless(GO_AVAILABLE, "go toolchain not found on PATH")
class TestSandboxSettingsAPI(_Base):
    def setUp(self):
        super().setUp()
        os.makedirs(os.path.join(self.sbx.ws, ".pando", "data"), exist_ok=True)
        port = _free_port()
        self.base = f"https://127.0.0.1:{port}"
        self.proc = subprocess.Popen(
            [PANDO_BIN, "serve", "--host", "127.0.0.1", "--port", str(port)],
            cwd=self.sbx.ws,
            env=self.sbx.env(),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        self.addCleanup(self._stop)
        deadline = time.time() + SERVE_STARTUP_TIMEOUT_SECONDS
        while time.time() < deadline:
            if self.proc.poll() is not None:
                self.fail(f"pando serve exited early with {self.proc.returncode}")
            try:
                status, body = _http("GET", self.base + "/api/v1/token")
                if status == 200 and isinstance(body, dict) and body.get("token"):
                    self.token = body["token"]
                    return
            except (urllib.error.URLError, ConnectionError, OSError):
                pass
            time.sleep(0.3)
        self.fail("pando serve did not become ready")

    def _stop(self):
        self.proc.terminate()
        try:
            self.proc.wait(timeout=15)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)

    def test_get_put_takes_effect_without_restart(self):
        url = self.base + "/api/v1/config/sandbox"
        status, body = _http("GET", url, self.token)
        self.assertEqual(status, 200, body)
        self.assertEqual(body["status"]["mode"], "workspace-write")
        self.assertIn("capability", body)

        status, _ = _http("GET", url)
        self.assertEqual(status, 401, "the sandbox settings endpoint must require the API token")

        cfg = dict(body["config"])
        cfg["mode"] = "read-only"
        status, body = _http("PUT", url, self.token, cfg)
        self.assertEqual(status, 200, body)
        self.assertEqual(body["status"]["mode"], "read-only")
        self.assertEqual(body["status"]["network"], "restricted")

        # Persisted to the GLOBAL config file: a separate CLI process sees it.
        self.assertEqual(self.sbx.status()["mode"], "read-only")

        cfg["disabled"] = True
        status, body = _http("PUT", url, self.token, cfg)
        self.assertEqual(status, 200, body)
        self.assertEqual(body["status"]["mode"], "off")
        self.assertFalse(body["status"]["active"])

        cfg["disabled"] = False
        cfg["mode"] = "no-such-mode"
        status, _ = _http("PUT", url, self.token, cfg)
        self.assertEqual(status, 400)


if __name__ == "__main__":
    unittest.main()
