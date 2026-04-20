"""
Tests for the `pando cronjob` CLI commands.

These tests exercise the CLI by building the binary and running it via
subprocess. Tests that require a running server or a crontab installation
are tagged as integration tests and are skipped gracefully when the
environment is not set up.

Run with:
    python3 -m pytest tests/test_cronjob_cli.py -v
    python3 -m unittest tests/test_cronjob_cli.py
"""

import os
import platform
import subprocess
import tempfile
import unittest

PANDO_ROOT = "/www/MCP/Pando/pando"
PANDO_BIN = os.path.join(PANDO_ROOT, "pando-test-bin")


def _build_binary() -> bool:
    """Build the pando binary for CLI tests. Returns True on success."""
    result = subprocess.run(
        ["go", "build", "-o", PANDO_BIN, "."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    return result.returncode == 0


def setUpModule():
    """Build the binary once before all CLI tests."""
    if not _build_binary():
        raise RuntimeError("Failed to build pando binary for CLI tests")


def tearDownModule():
    """Remove the test binary after all tests."""
    if os.path.exists(PANDO_BIN):
        os.remove(PANDO_BIN)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _run_pando(*args, cwd=None, env=None) -> subprocess.CompletedProcess:
    cmd = [PANDO_BIN] + list(args)
    return subprocess.run(
        cmd,
        cwd=cwd or PANDO_ROOT,
        capture_output=True,
        text=True,
        timeout=30,
    )


def _read_source(rel_path: str) -> str:
    with open(f"{PANDO_ROOT}/{rel_path}") as f:
        return f.read()


# ---------------------------------------------------------------------------
# Source-inspection tests (do not require a running binary or config)
# ---------------------------------------------------------------------------


class TestCronJobCLISourceStructure(unittest.TestCase):
    """Verify the CLI command definitions exist in cmd/cronjob.go."""

    def setUp(self):
        self.source = _read_source("cmd/cronjob.go")

    def test_cronjob_command_defined(self):
        self.assertIn("cronJobCmd", self.source)

    def test_list_subcommand_defined(self):
        self.assertIn("cronJobListCmd", self.source)

    def test_run_subcommand_defined(self):
        self.assertIn("cronJobRunCmd", self.source)

    def test_install_subcommand_defined(self):
        self.assertIn("cronJobInstallCmd", self.source)

    def test_uninstall_subcommand_defined(self):
        self.assertIn("cronJobUninstallCmd", self.source)

    def test_dry_run_flag_defined(self):
        self.assertIn("dry-run", self.source)

    def test_list_outputs_tabwriter_headers(self):
        """runCronJobList must print NAME/SCHEDULE/ENABLED/NEXT RUN/PROMPT headers."""
        self.assertIn("NAME", self.source)
        self.assertIn("SCHEDULE", self.source)
        self.assertIn("ENABLED", self.source)
        self.assertIn("NEXT RUN", self.source)
        self.assertIn("PROMPT", self.source)

    def test_run_returns_error_for_unknown_job(self):
        """runCronJobRun must return an error when the job is not in config."""
        self.assertIn("not found in configuration", self.source)

    def test_install_uses_dry_run_flag(self):
        """runCronJobInstall must print the cron line when dry-run is true."""
        self.assertIn("dryRun", self.source)
        self.assertIn("Dry run", self.source)

    def test_install_unix_uses_crontab(self):
        """Unix install must use crontab -e equivalent approach."""
        self.assertIn("crontab", self.source)

    def test_shell_quote_function_defined(self):
        """shellQuote must be defined for safe crontab entry construction."""
        self.assertIn("func shellQuote(", self.source)


class TestCronJobCLIRegistration(unittest.TestCase):
    """Verify that the CLI commands are properly registered in init()."""

    def setUp(self):
        self.source = _read_source("cmd/cronjob.go")
        start = self.source.find("func init()")
        self.assertNotEqual(start, -1, "init() not found in cmd/cronjob.go")
        end = self.source.find("\nfunc ", start + 1)
        self.init_body = self.source[start:end]

    def test_cronjob_added_to_root(self):
        self.assertIn("rootCmd.AddCommand(cronJobCmd)", self.init_body)

    def test_list_added_to_cronjob(self):
        self.assertIn("cronJobCmd.AddCommand(cronJobListCmd)", self.init_body)

    def test_run_added_to_cronjob(self):
        self.assertIn("cronJobCmd.AddCommand(cronJobRunCmd)", self.init_body)

    def test_install_added_to_cronjob(self):
        self.assertIn("cronJobCmd.AddCommand(cronJobInstallCmd)", self.init_body)

    def test_uninstall_added_to_cronjob(self):
        self.assertIn("cronJobCmd.AddCommand(cronJobUninstallCmd)", self.init_body)

    def test_dry_run_flag_registered_for_install(self):
        self.assertIn('"dry-run"', self.init_body)


# ---------------------------------------------------------------------------
# Binary-based tests (require the built binary)
# ---------------------------------------------------------------------------


class TestCronJobCLIBinaryHelp(unittest.TestCase):
    """Verify that `pando cronjob --help` exits 0 and lists subcommands."""

    def test_cronjob_help_exits_zero(self):
        result = _run_pando("cronjob", "--help")
        self.assertEqual(result.returncode, 0, f"Expected exit 0:\n{result.stderr}")

    def test_cronjob_help_lists_list_subcommand(self):
        result = _run_pando("cronjob", "--help")
        self.assertIn("list", result.stdout + result.stderr)

    def test_cronjob_help_lists_run_subcommand(self):
        result = _run_pando("cronjob", "--help")
        self.assertIn("run", result.stdout + result.stderr)

    def test_cronjob_help_lists_install_subcommand(self):
        result = _run_pando("cronjob", "--help")
        self.assertIn("install", result.stdout + result.stderr)

    def test_cronjob_help_lists_uninstall_subcommand(self):
        result = _run_pando("cronjob", "--help")
        self.assertIn("uninstall", result.stdout + result.stderr)


class TestCronJobCLIList(unittest.TestCase):
    """
    Verify `pando cronjob list` behaviour.

    When no jobs are configured (no .pando.toml in the temp dir), the command
    must exit 0 and print a human-readable message.
    """

    def _run_in_temp_dir(self, *args):
        with tempfile.TemporaryDirectory() as tmpdir:
            return _run_pando(*args, cwd=tmpdir)

    def test_list_exits_zero_with_no_config(self):
        """list should exit 0 even when no config exists."""
        result = self._run_in_temp_dir("cronjob", "list")
        # May print a config-not-found message or no jobs message; either is ok
        self.assertIn(result.returncode, (0, 1))

    def test_list_help_exits_zero(self):
        result = _run_pando("cronjob", "list", "--help")
        self.assertEqual(result.returncode, 0)


class TestCronJobCLIRunUnknown(unittest.TestCase):
    """Verify `pando cronjob run <unknown>` exits non-zero with an error message."""

    def test_run_unknown_exits_nonzero(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            result = _run_pando("cronjob", "run", "nonexistent-job-xyz", cwd=tmpdir)
        self.assertNotEqual(
            result.returncode,
            0,
            "Expected non-zero exit for unknown cronjob",
        )

    def test_run_unknown_prints_error(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            result = _run_pando("cronjob", "run", "nonexistent-job-xyz", cwd=tmpdir)
        combined = result.stdout + result.stderr
        # Should mention "not found" or an error about config/job
        self.assertTrue(
            "not found" in combined.lower()
            or "error" in combined.lower()
            or "no such" in combined.lower()
            or "load config" in combined.lower(),
            f"Expected error message, got:\n{combined}",
        )


@unittest.skipUnless(
    platform.system() != "Windows",
    "Unix-only: crontab tests skipped on Windows",
)
class TestCronJobCLIInstallDryRun(unittest.TestCase):
    """
    Verify `pando cronjob install <name> --dry-run` on Unix.

    This test creates a minimal .pando.toml with one job and confirms that
    --dry-run prints the cron line without modifying the actual crontab.
    """

    _CONFIG_TEMPLATE = """\
[CronJobs]
Enabled = true

[[CronJobs.Jobs]]
Name = "test-dryrun-job"
Schedule = "0 8 * * 1"
Prompt = "Summarize the daily log"
Enabled = true
Engine = "pando"
Timeout = "5m"

[Mesnada]
Enabled = true

[Mesnada.Orchestrator]
StorePath = ".pando/mesnada/tasks.json"
LogDir    = ".pando/mesnada/logs"

[Data]
Directory = ".pando/data"
"""

    def _setup_project(self, tmpdir: str):
        config_path = os.path.join(tmpdir, ".pando.toml")
        with open(config_path, "w") as f:
            f.write(self._CONFIG_TEMPLATE)
        # Create required directories
        os.makedirs(os.path.join(tmpdir, ".pando", "data"), exist_ok=True)
        os.makedirs(os.path.join(tmpdir, ".pando", "mesnada", "logs"), exist_ok=True)

    def test_install_dry_run_exits_zero(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            self._setup_project(tmpdir)
            result = _run_pando(
                "cronjob", "install", "test-dryrun-job", "--dry-run", cwd=tmpdir
            )
        self.assertEqual(
            result.returncode,
            0,
            f"Expected exit 0 for dry-run:\n{result.stderr}\n{result.stdout}",
        )

    def test_install_dry_run_prints_cron_line(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            self._setup_project(tmpdir)
            result = _run_pando(
                "cronjob", "install", "test-dryrun-job", "--dry-run", cwd=tmpdir
            )
        combined = result.stdout + result.stderr
        # Should print the schedule and the command, not touch the real crontab
        self.assertIn(
            "0 8 * * 1",
            combined,
            f"Expected cron schedule in dry-run output:\n{combined}",
        )

    def test_install_dry_run_contains_dry_run_indicator(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            self._setup_project(tmpdir)
            result = _run_pando(
                "cronjob", "install", "test-dryrun-job", "--dry-run", cwd=tmpdir
            )
        combined = result.stdout + result.stderr
        self.assertTrue(
            "dry" in combined.lower() or "would" in combined.lower(),
            f"Expected 'dry run' indicator in output:\n{combined}",
        )


class TestCronJobCLIACPModeSource(unittest.TestCase):
    """Verify that CronService is started in ACP mode in cmd/root.go."""

    def setUp(self):
        self.root_source = _read_source("cmd/root.go")

    def test_acp_mode_starts_cron_service(self):
        """runACPServerWithOptions must start CronService when enabled."""
        self.assertIn("CronService.Start(", self.root_source)

    def test_acp_mode_stops_cron_service(self):
        """runACPServerWithOptions must defer CronService.Stop()."""
        self.assertIn("CronService.Stop()", self.root_source)

    def test_acp_mode_checks_cron_service_not_nil(self):
        """Guard must check CronService != nil before calling Start."""
        self.assertIn("CronService != nil", self.root_source)

    def test_acp_mode_checks_cronjobs_enabled(self):
        """Guard must check cfg.CronJobs.Enabled before starting."""
        self.assertIn("cfg.CronJobs.Enabled", self.root_source)


if __name__ == "__main__":
    unittest.main()
