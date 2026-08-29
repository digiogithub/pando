"""
End-to-end tests for the Desktop Controller (internal/uiauto) MCP exposure.

These tests build the real `pando` binary and drive `pando mcp-server` over its
real stdio JSON-RPC transport (the exact interface an external MCP client uses),
exactly the way `tests/test_cronjob_cli.py` drives the `pando cronjob` CLI. They
are honest about the environment they run in: this is expected to run on a
headless/tty Linux box with no GUI application and often no X11/Wayland display
session, so assertions accept the full set of structured error codes the real
Desktop Controller reports in that state (PLATFORM_NOT_SUPPORTED, APP_NOT_FOUND,
ACTION_FAILED wrapping a physical-input PLATFORM_NOT_SUPPORTED, etc.) rather than
assuming a single "happy path" a headless CI box cannot actually reach. No test
here pretends this box can click a real button in a real GUI app.

Covers:
- MCP exposure is gated by [InternalTools] DesktopEnabled: the 12 desktop_*
  tools are entirely absent from tools/list when disabled, and all present
  (with sane input schemas) when enabled.
- desktop_apps succeeds structurally (ok:true with apps/windows lists, or an
  honest ok:false PLATFORM_NOT_SUPPORTED when no backend is available).
- desktop_find against a nonexistent application reports a structured error
  (APP_NOT_FOUND when a backend is reachable, PLATFORM_NOT_SUPPORTED when none
  is) -- never a bare/opaque error string.
- desktop_find with a malformed selector reports INVALID_ARGS.
- desktop_click / desktop_read against an unknown qualified ref reports
  SNAPSHOT_NOT_FOUND (or PLATFORM_NOT_SUPPORTED).
- desktop_click_at is coordinate-validated and/or fails honestly through the
  physical-input layer, never silently "succeeding" on a display-less box.
- Every desktop_* response is well-formed structured content: valid JSON-RPC,
  a boolean "ok" field, and on failure a {code, message, suggestion} error
  object using one of the documented DesktopError codes.

Run with:
    python3 -m pytest tests/test_desktop_controller_mcp_e2e.py -v
    python3 -m unittest tests/test_desktop_controller_mcp_e2e.py
"""

import json
import os
import shutil
import subprocess
import tempfile
import textwrap
import unittest

PANDO_ROOT = "/www/MCP/Pando/pando"
PANDO_BIN = os.path.join(PANDO_ROOT, "pando-desktop-e2e-bin")

# All 12 desktop_* tools the Desktop Controller registers when DesktopEnabled.
EXPECTED_DESKTOP_TOOLS = {
    "desktop_apps",
    "desktop_observe",
    "desktop_find",
    "desktop_read",
    "desktop_click",
    "desktop_type",
    "desktop_key",
    "desktop_scroll",
    "desktop_focus",
    "desktop_wait",
    "desktop_screenshot",
    "desktop_click_at",
}

# Structured error codes the DesktopController's core.DesktopError vocabulary
# defines (internal/uiauto/core/errors.go). Any error surfaced by a desktop_*
# tool must use one of these -- never a bare Go error string.
KNOWN_DESKTOP_ERROR_CODES = {
    "PERM_DENIED",
    "ELEMENT_NOT_FOUND",
    "APP_NOT_FOUND",
    "STALE_REF",
    "SNAPSHOT_NOT_FOUND",
    "POLICY_DENIED",
    "ACTION_FAILED",
    "PLATFORM_NOT_SUPPORTED",
    "TIMEOUT",
    "INVALID_ARGS",
}

MCP_STDIO_TIMEOUT_SECONDS = 20


def setUpModule():
    """Build the pando binary once before all tests in this module."""
    result = subprocess.run(
        ["go", "build", "-o", PANDO_BIN, "."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        raise RuntimeError(
            "Failed to build pando binary for Desktop Controller e2e tests:\n"
            + result.stdout
            + result.stderr
        )


def tearDownModule():
    if os.path.exists(PANDO_BIN):
        os.remove(PANDO_BIN)


def _write_project(tmpdir: str, internal_tools_toml: str) -> None:
    """Write a minimal .pando.toml project so `pando mcp-server` can start
    without a full interactive setup -- mirrors the minimal project shape
    tests/test_cronjob_cli.py's install-dry-run test uses."""
    config_path = os.path.join(tmpdir, ".pando.toml")
    with open(config_path, "w") as f:
        f.write(
            textwrap.dedent(
                f"""\
                {internal_tools_toml}

                [Data]
                Directory = ".pando/data"
                """
            )
        )
    os.makedirs(os.path.join(tmpdir, ".pando", "data"), exist_ok=True)


def _mcp_stdio_roundtrip(tmpdir: str, requests: list) -> dict:
    """
    Starts `pando mcp-server --no-http --cwd <tmpdir>` (the real, external MCP
    stdio transport), writes one JSON-RPC request per line, closes stdin, and
    returns {id: response_dict} for every request that carried an "id" (i.e.
    every non-notification).
    """
    proc = subprocess.Popen(
        [PANDO_BIN, "mcp-server", "--no-http", "--cwd", tmpdir],
        cwd=tmpdir,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    payload = "\n".join(json.dumps(r) for r in requests) + "\n"
    try:
        stdout, stderr = proc.communicate(input=payload, timeout=MCP_STDIO_TIMEOUT_SECONDS)
    except subprocess.TimeoutExpired:
        proc.kill()
        stdout, stderr = proc.communicate()
        raise AssertionError(
            f"pando mcp-server did not exit within {MCP_STDIO_TIMEOUT_SECONDS}s.\n"
            f"stdout so far:\n{stdout}\nstderr:\n{stderr}"
        )

    responses = {}
    for line in stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        if "id" in obj and obj["id"] is not None:
            responses[obj["id"]] = obj
    return responses


def _init_request(rid=1):
    return {
        "jsonrpc": "2.0",
        "id": rid,
        "method": "initialize",
        "params": {"protocolVersion": "2025-06-18"},
    }


def _tools_list_request(rid=2):
    return {"jsonrpc": "2.0", "id": rid, "method": "tools/list"}


def _tools_call_request(rid, name, arguments):
    return {
        "jsonrpc": "2.0",
        "id": rid,
        "method": "tools/call",
        "params": {"name": name, "arguments": arguments},
    }


def _tool_result_payload(response: dict) -> dict:
    """Extracts and JSON-decodes the structured {ok, ...} payload a desktop_*
    tool response carries as its first text content block (TOON/TOML/JSON,
    per NewStructuredResponse -- our requests don't set an Accept format, so
    this normalizes either by trying JSON first and falling back to a
    lenient TOML-ish key: value line scan for the common case)."""
    result = response.get("result")
    assert result is not None, f"tool call produced no result: {response}"
    content = result.get("content")
    assert content, f"tool call result has no content: {response}"
    text = content[0]["text"]
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        pass
    # NewStructuredResponse defaults to a compact TOML-ish "key: value" /
    # nested-block rendering when no explicit format is requested. Parse just
    # enough of it to recover ok/error.code for assertions.
    parsed = {}
    error = {}
    in_error = False
    for raw_line in text.splitlines():
        if not raw_line.strip():
            continue
        indented = raw_line.startswith("  ")
        line = raw_line.strip()
        if line == "error:":
            in_error = True
            continue
        if ":" not in line:
            continue
        key, _, value = line.partition(":")
        key = key.strip()
        value = value.strip().strip('"')
        if in_error and indented:
            error[key] = value
        else:
            in_error = False
            if value == "true":
                value = True
            elif value == "false":
                value = False
            parsed[key] = value
    if error:
        parsed["error"] = error
    return parsed


class TestDesktopControllerMCPGating(unittest.TestCase):
    """DesktopEnabled must gate MCP exposure exactly, both ways."""

    def test_desktop_tools_absent_when_disabled(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            _write_project(tmpdir, "[InternalTools]\nDesktopEnabled = false")
            responses = _mcp_stdio_roundtrip(
                tmpdir, [_init_request(1), _tools_list_request(2)]
            )
        self.assertIn(2, responses)
        tool_names = {t["name"] for t in responses[2]["result"]["tools"]}
        self.assertEqual(
            tool_names & EXPECTED_DESKTOP_TOOLS,
            set(),
            "no desktop_* tool should be exposed over MCP when DesktopEnabled=false",
        )

    def test_desktop_tools_absent_by_default(self):
        """No [InternalTools] section at all: DesktopEnabled defaults to false."""
        with tempfile.TemporaryDirectory() as tmpdir:
            _write_project(tmpdir, "")
            responses = _mcp_stdio_roundtrip(
                tmpdir, [_init_request(1), _tools_list_request(2)]
            )
        tool_names = {t["name"] for t in responses[2]["result"]["tools"]}
        self.assertEqual(tool_names & EXPECTED_DESKTOP_TOOLS, set())

    def test_all_twelve_desktop_tools_present_when_enabled(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            _write_project(tmpdir, "[InternalTools]\nDesktopEnabled = true")
            responses = _mcp_stdio_roundtrip(
                tmpdir, [_init_request(1), _tools_list_request(2)]
            )
        tools_by_name = {t["name"]: t for t in responses[2]["result"]["tools"]}
        present = set(tools_by_name) & EXPECTED_DESKTOP_TOOLS
        self.assertEqual(
            present,
            EXPECTED_DESKTOP_TOOLS,
            f"missing desktop tools: {EXPECTED_DESKTOP_TOOLS - present}",
        )

    def test_desktop_click_requires_ref_in_schema(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            _write_project(tmpdir, "[InternalTools]\nDesktopEnabled = true")
            responses = _mcp_stdio_roundtrip(
                tmpdir, [_init_request(1), _tools_list_request(2)]
            )
        tools_by_name = {t["name"]: t for t in responses[2]["result"]["tools"]}
        click_schema = tools_by_name["desktop_click"]["inputSchema"]
        self.assertIn("ref", click_schema.get("required", []))

    def test_desktop_find_requires_selector_in_schema(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            _write_project(tmpdir, "[InternalTools]\nDesktopEnabled = true")
            responses = _mcp_stdio_roundtrip(
                tmpdir, [_init_request(1), _tools_list_request(2)]
            )
        tools_by_name = {t["name"]: t for t in responses[2]["result"]["tools"]}
        find_schema = tools_by_name["desktop_find"]["inputSchema"]
        self.assertIn("selector", find_schema.get("required", []))

    def test_non_desktop_builtin_tools_still_present(self):
        """Sanity check: enabling Desktop must not crowd out unrelated tools."""
        with tempfile.TemporaryDirectory() as tmpdir:
            _write_project(tmpdir, "[InternalTools]\nDesktopEnabled = true")
            responses = _mcp_stdio_roundtrip(
                tmpdir, [_init_request(1), _tools_list_request(2)]
            )
        tool_names = {t["name"] for t in responses[2]["result"]["tools"]}
        self.assertIn("cache_read", tool_names)


class TestDesktopControllerStructuredErrors(unittest.TestCase):
    """
    Exercise the real desktop_* tool surface through the real MCP transport.
    This machine is headless (no GUI app registered on the accessibility bus,
    and typically no X11/WAYLAND_DISPLAY session), so these tests assert
    honest structured-error behaviour rather than a live GUI interaction --
    per the task, we do not pretend a headless tty box can click a button.
    """

    @classmethod
    def setUpClass(cls):
        cls.tmpdir = tempfile.mkdtemp(prefix="pando-desktop-e2e-")
        _write_project(
            cls.tmpdir,
            textwrap.dedent(
                """\
                [InternalTools]
                DesktopEnabled = true
                DesktopBackend = 'auto'
                DesktopAllowPhysicalInput = true
                DesktopMaxNodes = 500
                DesktopDefaultDepth = 3
                DesktopActionTimeout = 5
                DesktopSnapshotTTL = 60
                """
            ),
        )

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmpdir, ignore_errors=True)

    def _call(self, name, arguments, rid=2):
        responses = _mcp_stdio_roundtrip(
            self.tmpdir,
            [_init_request(1), _tools_call_request(rid, name, arguments)],
        )
        self.assertIn(rid, responses, f"no response for {name} call")
        return responses[rid]

    def test_desktop_apps_returns_well_formed_response(self):
        response = self._call("desktop_apps", {})
        payload = _tool_result_payload(response)
        self.assertIn("ok", payload)
        if payload["ok"] in (True, "true"):
            # Honest empty-desktop outcome (no GUI apps registered) is valid;
            # a non-empty list would be equally valid on a box with a GUI.
            self.assertIn("apps", payload)
            self.assertIn("windows", payload)
        else:
            self._assert_structured_error(payload, {"PLATFORM_NOT_SUPPORTED"})

    def test_desktop_find_nonexistent_app_reports_structured_error(self):
        response = self._call(
            "desktop_find",
            {"selector": "button", "app_id": "nonexistent-app-xyz-12345"},
        )
        payload = _tool_result_payload(response)
        self.assertIn(payload.get("ok"), (False, "false"))
        self._assert_structured_error(payload, {"APP_NOT_FOUND", "PLATFORM_NOT_SUPPORTED"})

    def test_desktop_find_malformed_selector_is_invalid_args(self):
        response = self._call("desktop_find", {"selector": "###not a valid selector((("})
        payload = _tool_result_payload(response)
        self.assertIn(payload.get("ok"), (False, "false"))
        self._assert_structured_error(
            payload, {"INVALID_ARGS", "PLATFORM_NOT_SUPPORTED"}
        )

    def test_desktop_click_unknown_ref_reports_structured_error(self):
        response = self._call("desktop_click", {"ref": "@sBOGUSSNAP:e1"})
        payload = _tool_result_payload(response)
        self.assertIn(payload.get("ok"), (False, "false"))
        self._assert_structured_error(
            payload, {"SNAPSHOT_NOT_FOUND", "PLATFORM_NOT_SUPPORTED", "INVALID_ARGS"}
        )

    def test_desktop_read_malformed_ref_is_invalid_args_or_not_found(self):
        response = self._call("desktop_read", {"ref": "not-a-qualified-ref"})
        payload = _tool_result_payload(response)
        self.assertIn(payload.get("ok"), (False, "false"))
        self._assert_structured_error(
            payload,
            {"INVALID_ARGS", "SNAPSHOT_NOT_FOUND", "PLATFORM_NOT_SUPPORTED"},
        )

    def test_desktop_click_at_never_silently_succeeds_headless(self):
        """
        On a genuinely headless box (no DISPLAY/WAYLAND_DISPLAY), a coordinate
        click cannot possibly land anywhere real. The Manager either rejects
        it (POLICY_DENIED/INVALID_ARGS) or attempts the physical layer and
        gets an honest PLATFORM_NOT_SUPPORTED wrapped as ACTION_FAILED -- it
        must never report ok:true, which would be a lie on this box.
        """
        response = self._call("desktop_click_at", {"x": 50, "y": 50})
        payload = _tool_result_payload(response)
        has_display = bool(
            os.environ.get("DISPLAY") or os.environ.get("WAYLAND_DISPLAY")
        )
        if has_display:
            self.skipTest(
                "a real display session is present on this runner; "
                "click_at may legitimately succeed here"
            )
        self.assertIn(
            payload.get("ok"),
            (False, "false"),
            f"desktop_click_at reported success with no display session: {payload}",
        )
        self._assert_structured_error(
            payload,
            {"PLATFORM_NOT_SUPPORTED", "POLICY_DENIED", "ACTION_FAILED", "INVALID_ARGS"},
        )

    def test_desktop_click_at_out_of_bounds_coordinate(self):
        """A wildly out-of-range coordinate must never be silently clamped."""
        response = self._call("desktop_click_at", {"x": 10**7, "y": 10**7})
        payload = _tool_result_payload(response)
        self.assertIn(payload.get("ok"), (False, "false"))
        self._assert_structured_error(
            payload,
            {"INVALID_ARGS", "POLICY_DENIED", "ACTION_FAILED", "PLATFORM_NOT_SUPPORTED"},
        )

    def test_unknown_tool_name_is_not_a_desktop_crash(self):
        """Calling a nonexistent tool must fail cleanly via JSON-RPC, not hang
        or crash the desktop-enabled server."""
        responses = _mcp_stdio_roundtrip(
            self.tmpdir,
            [
                _init_request(1),
                _tools_call_request(2, "desktop_does_not_exist", {}),
            ],
        )
        self.assertIn(2, responses)
        response = responses[2]
        # Either a JSON-RPC error or an isError tool result is acceptable;
        # what matters is the process replied instead of hanging/crashing.
        self.assertTrue(
            response.get("error") is not None
            or response.get("result", {}).get("isError"),
            f"expected an error for an unknown tool, got: {response}",
        )

    def _assert_structured_error(self, payload: dict, acceptable_codes: set):
        self.assertIn("error", payload, f"expected a structured error in {payload}")
        error = payload["error"]
        self.assertIn("code", error)
        self.assertIn(
            error["code"],
            KNOWN_DESKTOP_ERROR_CODES,
            f"error code {error['code']!r} is not a documented DesktopError code",
        )
        self.assertIn(
            error["code"],
            acceptable_codes,
            f"expected one of {acceptable_codes} for this scenario, got {error['code']!r} "
            f"(message: {error.get('message')!r})",
        )
        self.assertIn("message", error)
        self.assertTrue(error["message"], "error message must not be empty")


if __name__ == "__main__":
    unittest.main()
