"""
Tests for CronJob config validation logic.

These tests verify that the Go config validation rules for CronJobs are
correctly implemented. They inspect the source directly (struct definitions,
validation function) and run Go tests to confirm runtime behaviour.

Run with:
    python3 -m pytest tests/test_cronjob_config.py -v
    python3 -m unittest tests/test_cronjob_config.py
"""

import subprocess
import unittest

PANDO_ROOT = "/www/MCP/Pando/pando"


# ---------------------------------------------------------------------------
# pytest-style free functions (also collected by unittest via the class below)
# ---------------------------------------------------------------------------


def test_build_succeeds():
    """go build ./... must succeed after all CronJob phases are implemented."""
    result = subprocess.run(
        ["go", "build", "./..."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"Build failed:\n{result.stderr}"


def test_config_package_tests_pass():
    """The config package Go tests must pass (covers ValidateCronJobsConfig)."""
    result = subprocess.run(
        ["go", "test", "./internal/config/..."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, (
        f"Config tests failed:\n{result.stderr}\n{result.stdout}"
    )


def test_cronjob_package_tests_pass():
    """The cronjob package Go tests must pass."""
    result = subprocess.run(
        ["go", "test", "./internal/cronjob/..."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
        timeout=120,
    )
    assert result.returncode == 0, (
        f"CronJob tests failed:\n{result.stderr}\n{result.stdout}"
    )


# ---------------------------------------------------------------------------
# Source-inspection helpers
# ---------------------------------------------------------------------------


def _read_source(rel_path: str) -> str:
    with open(f"{PANDO_ROOT}/{rel_path}") as f:
        return f.read()


# ---------------------------------------------------------------------------
# unittest classes
# ---------------------------------------------------------------------------


class TestCronJobConfigSourceStructure(unittest.TestCase):
    """Verify that the CronJob and CronJobsConfig struct fields exist in source."""

    def setUp(self):
        self.config_source = _read_source("internal/config/config.go")

    def test_cronjob_struct_defined(self):
        self.assertIn("type CronJob struct", self.config_source)

    def test_cronjobsconfig_struct_defined(self):
        self.assertIn("type CronJobsConfig struct", self.config_source)

    def test_cronjob_has_name_field(self):
        self.assertIn('Name', self.config_source)

    def test_cronjob_has_schedule_field(self):
        self.assertIn('Schedule', self.config_source)

    def test_cronjob_has_prompt_field(self):
        self.assertIn('Prompt', self.config_source)

    def test_cronjob_has_enabled_field(self):
        self.assertIn('Enabled', self.config_source)

    def test_cronjob_has_engine_field(self):
        self.assertIn('Engine', self.config_source)

    def test_cronjob_has_tags_field(self):
        self.assertIn('Tags', self.config_source)

    def test_cronjob_has_timeout_field(self):
        self.assertIn('Timeout', self.config_source)

    def test_validate_cronjobs_config_exported(self):
        self.assertIn('func ValidateCronJobsConfig', self.config_source)

    def test_parse_cron_expression_exported(self):
        self.assertIn('func ParseCronExpression', self.config_source)


class TestCronJobConfigValidationRules(unittest.TestCase):
    """Verify validation rules are encoded in the source."""

    def setUp(self):
        self.config_source = _read_source("internal/config/config.go")
        # Locate validateCronJobs body
        start = self.config_source.find("func validateCronJobs(")
        self.assertNotEqual(start, -1, "validateCronJobs not found")
        end = self.config_source.find("\nfunc ", start + 1)
        self.validate_body = self.config_source[start:end]

    def test_empty_name_is_rejected(self):
        """Validation must reject an empty job name."""
        self.assertIn("name is required", self.validate_body)

    def test_duplicate_name_is_rejected(self):
        """Validation must detect duplicate job names."""
        self.assertIn("duplicate cronjob name", self.validate_body)

    def test_invalid_schedule_is_rejected(self):
        """Validation must parse and reject invalid cron schedules."""
        self.assertIn("ParseCronExpression", self.validate_body)

    def test_empty_prompt_is_rejected(self):
        """Validation must reject jobs with an empty prompt."""
        self.assertIn("prompt is required", self.validate_body)


class TestCronJobConfigInitTemplate(unittest.TestCase):
    """Verify the DefaultConfigTemplate includes an example CronJobs block."""

    def setUp(self):
        self.init_source = _read_source("internal/config/init.go")
        # Locate DefaultConfigTemplate
        start = self.init_source.find("const DefaultConfigTemplate")
        self.assertNotEqual(start, -1, "DefaultConfigTemplate not found")
        end = self.init_source.find("\n`", start)
        self.template = self.init_source[start:end]

    def test_cronjobs_section_comment_present(self):
        self.assertIn("CronJobs", self.template)

    def test_cronjobs_enabled_example_present(self):
        self.assertIn("Enabled = true", self.template)

    def test_cronjobs_jobs_example_present(self):
        self.assertIn("CronJobs.Jobs", self.template)

    def test_cronjobs_schedule_example_present(self):
        self.assertIn("Schedule", self.template)

    def test_cronjobs_prompt_example_present(self):
        self.assertIn("Prompt", self.template)

    def test_cronjobs_timeout_example_present(self):
        self.assertIn("Timeout", self.template)

    def test_cronjobs_tags_example_present(self):
        self.assertIn("Tags", self.template)


class TestCronJobConfigGoValidation(unittest.TestCase):
    """Run Go-level tests to confirm validation behaviour at runtime."""

    def _go_test(self, *extra_args):
        cmd = ["go", "test", "./internal/config/..."] + list(extra_args)
        return subprocess.run(
            cmd,
            cwd=PANDO_ROOT,
            capture_output=True,
            text=True,
            timeout=120,
        )

    def test_valid_job_passes_config_tests(self):
        """Running Go config tests verifies that a valid job passes."""
        result = self._go_test()
        self.assertEqual(
            result.returncode, 0,
            f"Config tests failed:\n{result.stderr}\n{result.stdout}",
        )

    def test_cronjob_package_compiles(self):
        """The cronjob package must compile cleanly."""
        result = subprocess.run(
            ["go", "build", "./internal/cronjob/..."],
            cwd=PANDO_ROOT,
            capture_output=True,
            text=True,
        )
        self.assertEqual(
            result.returncode, 0,
            f"cronjob package build failed:\n{result.stderr}",
        )

    def test_vet_cronjob_package(self):
        """go vet must pass on the cronjob package."""
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


class TestCronJobConfigDisabledSection(unittest.TestCase):
    """Verify that a disabled CronJobs section is handled in source and build."""

    def setUp(self):
        self.service_source = _read_source("internal/cronjob/service.go")

    def test_schedule_locked_respects_enabled_flag(self):
        """scheduleLocked must short-circuit when Enabled is false."""
        # Locate scheduleLocked body
        start = self.service_source.find("func (s *Service) scheduleLocked()")
        self.assertNotEqual(start, -1, "scheduleLocked not found")
        end = self.service_source.find("\nfunc ", start + 1)
        body = self.service_source[start:end]
        self.assertIn("!s.cfg.Enabled", body)

    def test_service_skips_disabled_jobs(self):
        """scheduleLocked must skip jobs where Enabled is false."""
        start = self.service_source.find("func (s *Service) scheduleLocked()")
        self.assertNotEqual(start, -1)
        end = self.service_source.find("\nfunc ", start + 1)
        body = self.service_source[start:end]
        self.assertIn("!job.Enabled", body)


if __name__ == "__main__":
    unittest.main()
