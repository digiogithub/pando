"""
Tests for CronJob service behaviour.

These tests verify the Service implementation via source inspection and Go
test execution. Where the service requires a running server (REST API), the
tests are documented as integration tests and skipped when the server is not
available — following the same approach used in other test files in this suite.

Run with:
    python3 -m pytest tests/test_cronjob_service.py -v
    python3 -m unittest tests/test_cronjob_service.py
"""

import subprocess
import unittest

PANDO_ROOT = "/www/MCP/Pando/pando"


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _read_source(rel_path: str) -> str:
    with open(f"{PANDO_ROOT}/{rel_path}") as f:
        return f.read()


def _go_build(pkg: str) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["go", "build", pkg],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )


def _go_test(pkg: str, timeout: int = 120) -> subprocess.CompletedProcess:
    return subprocess.run(
        ["go", "test", pkg],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
        timeout=timeout,
    )


# ---------------------------------------------------------------------------
# pytest-style free functions
# ---------------------------------------------------------------------------


def test_cronjob_service_builds():
    """The cronjob service package must compile without errors."""
    result = _go_build("./internal/cronjob/...")
    assert result.returncode == 0, f"Build failed:\n{result.stderr}"


def test_cronjob_service_tests_pass():
    """All Go tests in the cronjob package must pass."""
    result = _go_test("./internal/cronjob/...")
    assert result.returncode == 0, (
        f"CronJob service tests failed:\n{result.stderr}\n{result.stdout}"
    )


# ---------------------------------------------------------------------------
# Source-inspection tests
# ---------------------------------------------------------------------------


class TestCronJobServiceSourceStructure(unittest.TestCase):
    """Verify that the Service struct and its public API exist in source."""

    def setUp(self):
        self.service_source = _read_source("internal/cronjob/service.go")

    def test_service_struct_defined(self):
        self.assertIn("type Service struct", self.service_source)

    def test_new_service_constructor_defined(self):
        self.assertIn("func NewService(", self.service_source)

    def test_start_method_defined(self):
        self.assertIn("func (s *Service) Start(", self.service_source)

    def test_stop_method_defined(self):
        self.assertIn("func (s *Service) Stop()", self.service_source)

    def test_reload_method_defined(self):
        self.assertIn("func (s *Service) Reload(", self.service_source)

    def test_run_now_method_defined(self):
        self.assertIn("func (s *Service) RunNow(", self.service_source)

    def test_list_jobs_method_defined(self):
        self.assertIn("func (s *Service) ListJobs()", self.service_source)

    def test_subscribe_method_defined(self):
        self.assertIn("func (s *Service) Subscribe(", self.service_source)

    def test_job_status_struct_defined(self):
        self.assertIn("type JobStatus struct", self.service_source)

    def test_job_status_has_next_run_field(self):
        self.assertIn("NextRun", self.service_source)


class TestCronJobServiceRunNowBehaviour(unittest.TestCase):
    """Verify RunNow error handling in source."""

    def setUp(self):
        self.service_source = _read_source("internal/cronjob/service.go")
        start = self.service_source.find("func (s *Service) RunNow(")
        self.assertNotEqual(start, -1, "RunNow not found")
        end = self.service_source.find("\nfunc ", start + 1)
        self.run_now_body = self.service_source[start:end]

    def test_run_now_returns_error_for_unknown_job(self):
        """RunNow must return an error when the job is not found."""
        self.assertIn("not found", self.run_now_body)

    def test_run_now_looks_up_job_by_name(self):
        """RunNow must search for the job by name."""
        self.assertIn("jobByNameLocked", self.run_now_body)

    def test_run_now_delegates_to_runner(self):
        """RunNow must delegate execution to the runner."""
        self.assertIn("s.runner.run(", self.run_now_body)


class TestCronJobServiceListJobs(unittest.TestCase):
    """Verify ListJobs behaviour in source."""

    def setUp(self):
        self.service_source = _read_source("internal/cronjob/service.go")
        start = self.service_source.find("func (s *Service) ListJobs()")
        self.assertNotEqual(start, -1, "ListJobs not found")
        end = self.service_source.find("\nfunc ", start + 1)
        self.list_jobs_body = self.service_source[start:end]

    def test_list_jobs_reads_all_configured_jobs(self):
        """ListJobs must iterate over s.cfg.Jobs."""
        self.assertIn("s.cfg.Jobs", self.list_jobs_body)

    def test_list_jobs_includes_next_run(self):
        """ListJobs must populate NextRun for scheduled jobs."""
        self.assertIn("NextRun", self.list_jobs_body)

    def test_list_jobs_sorts_results(self):
        """ListJobs must return jobs sorted by name."""
        self.assertIn("sort.Slice", self.list_jobs_body)

    def test_list_jobs_returns_job_status_slice(self):
        """ListJobs must return a []JobStatus."""
        self.assertIn("[]JobStatus", self.list_jobs_body)


class TestCronJobServiceReload(unittest.TestCase):
    """Verify Reload / reloadLocked behaviour in source."""

    def setUp(self):
        self.service_source = _read_source("internal/cronjob/service.go")
        start = self.service_source.find("func (s *Service) reloadLocked(")
        self.assertNotEqual(start, -1, "reloadLocked not found")
        end = self.service_source.find("\nfunc ", start + 1)
        self.reload_body = self.service_source[start:end]

    def test_reload_validates_config(self):
        """reloadLocked must validate the new config before applying it."""
        self.assertIn("ValidateCronJobsConfig", self.reload_body)

    def test_reload_removes_old_entries(self):
        """reloadLocked must remove all previously scheduled cron entries."""
        self.assertIn("s.cron.Remove(", self.reload_body)

    def test_reload_calls_schedule_locked(self):
        """reloadLocked must reschedule jobs after clearing old entries."""
        self.assertIn("s.scheduleLocked()", self.reload_body)

    def test_reload_logs_event(self):
        """reloadLocked must emit a cronjob_event=reload log."""
        self.assertIn("cronjob_event=reload", self.reload_body)

    def test_reload_logs_jobs_count(self):
        """reloadLocked must log the number of jobs after reload."""
        self.assertIn("jobs_count", self.reload_body)


class TestCronJobServiceSlogFormat(unittest.TestCase):
    """Verify all required slog events are present in the service source files."""

    def setUp(self):
        self.service_source = _read_source("internal/cronjob/service.go")
        self.runner_source = _read_source("internal/cronjob/runner.go")

    # service.go events

    def test_scheduled_event_present_in_service(self):
        self.assertIn("cronjob_event=scheduled", self.service_source)

    def test_scheduled_event_has_name_key(self):
        # Confirm "name" key appears near the scheduled event
        idx = self.service_source.find("cronjob_event=scheduled")
        self.assertNotEqual(idx, -1)
        snippet = self.service_source[idx : idx + 120]
        self.assertIn('"name"', snippet)

    def test_scheduled_event_has_schedule_key(self):
        idx = self.service_source.find("cronjob_event=scheduled")
        snippet = self.service_source[idx : idx + 120]
        self.assertIn('"schedule"', snippet)

    def test_reload_event_present_in_service(self):
        self.assertIn("cronjob_event=reload", self.service_source)

    def test_reload_event_has_jobs_count_key(self):
        idx = self.service_source.find("cronjob_event=reload")
        snippet = self.service_source[idx : idx + 80]
        self.assertIn("jobs_count", snippet)

    # runner.go events

    def test_fired_event_present_in_runner(self):
        self.assertIn("cronjob_event=fired", self.runner_source)

    def test_fired_event_has_name_key(self):
        idx = self.runner_source.find("cronjob_event=fired")
        snippet = self.runner_source[idx : idx + 80]
        self.assertIn('"name"', snippet)

    def test_fired_event_has_task_id_key(self):
        idx = self.runner_source.find("cronjob_event=fired")
        snippet = self.runner_source[idx : idx + 80]
        self.assertIn("task_id", snippet)

    def test_skipped_event_present_in_runner(self):
        self.assertIn("cronjob_event=skipped", self.runner_source)

    def test_skipped_event_has_reason_already_running(self):
        idx = self.runner_source.find("cronjob_event=skipped")
        snippet = self.runner_source[idx : idx + 120]
        self.assertIn("already_running", snippet)

    def test_error_event_present_in_runner(self):
        self.assertIn("cronjob_event=error", self.runner_source)


class TestCronJobServiceGoTests(unittest.TestCase):
    """Run the Go test suite for the cronjob package."""

    def test_go_tests_pass(self):
        result = _go_test("./internal/cronjob/...")
        self.assertEqual(
            result.returncode, 0,
            f"Go cronjob tests failed:\n{result.stderr}\n{result.stdout}",
        )

    def test_go_vet_passes(self):
        result = subprocess.run(
            ["go", "vet", "./internal/cronjob/..."],
            cwd=PANDO_ROOT,
            capture_output=True,
            text=True,
        )
        self.assertEqual(
            result.returncode, 0,
            f"go vet failed:\n{result.stderr}",
        )


if __name__ == "__main__":
    unittest.main()
