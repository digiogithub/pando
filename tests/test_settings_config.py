"""
Integration tests for the Settings TUI — Full Config Coverage.

Verifies that:
- The project builds successfully (all 14 sections wired in buildSections)
- The config package tests pass
- The new Update* functions and TUI sections integrate correctly

Run with:
    python3 -m pytest tests/test_settings_config.py -v
    python3 -m unittest tests/test_settings_config.py
"""

import subprocess
import unittest

PANDO_ROOT = "/www/MCP/Pando/pando"


def test_build_succeeds():
    """Ensure go build ./... completes without errors after all phases are wired."""
    result = subprocess.run(
        ["go", "build", "./..."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, f"Build failed:\n{result.stderr}"


def test_config_tests_pass():
    """Run the Go tests for the config package to verify Update* functions."""
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


class TestBuildIntegration(unittest.TestCase):
    """Smoke tests that verify the full project builds and Go tests pass."""

    def test_build_succeeds(self):
        """go build ./... must succeed with all 14 sections wired in buildSections."""
        result = subprocess.run(
            ["go", "build", "./..."],
            cwd=PANDO_ROOT,
            capture_output=True,
            text=True,
        )
        self.assertEqual(
            result.returncode,
            0,
            f"Build failed:\n{result.stderr}",
        )

    def test_config_tests_pass(self):
        """Config package Go tests must pass (validates Update* functions)."""
        result = subprocess.run(
            ["go", "test", "./internal/config/..."],
            cwd=PANDO_ROOT,
            capture_output=True,
            text=True,
            timeout=120,
        )
        self.assertEqual(
            result.returncode,
            0,
            f"Config tests failed:\n{result.stderr}\n{result.stdout}",
        )

    def test_tui_settings_page_compiles(self):
        """The TUI settings page package must compile cleanly."""
        result = subprocess.run(
            ["go", "build", "./internal/tui/page/..."],
            cwd=PANDO_ROOT,
            capture_output=True,
            text=True,
        )
        self.assertEqual(
            result.returncode,
            0,
            f"TUI settings page build failed:\n{result.stderr}",
        )


class TestBuildSectionsWiring(unittest.TestCase):
    """Verify that all 14 sections are present in buildSections() and all save cases exist."""

    def setUp(self):
        settings_path = f"{PANDO_ROOT}/internal/tui/page/settings.go"
        with open(settings_path, "r") as f:
            self.source = f.read()

        # Locate buildSections function body
        start = self.source.find("func buildSections(")
        self.assertNotEqual(start, -1, "buildSections not found in settings.go")
        self.sections_snippet = self.source[start : start + 1000]

        # Locate persistSetting function body
        persist_start = self.source.find("func persistSetting(")
        self.assertNotEqual(persist_start, -1, "persistSetting not found in settings.go")
        persist_end = self.source.find("\nfunc ", persist_start + 1)
        self.persist_body = self.source[persist_start:persist_end]

    # --- buildSections coverage ---

    def test_build_general_section_present(self):
        self.assertIn("buildGeneralSection", self.sections_snippet)

    def test_build_skills_section_present(self):
        self.assertIn("buildSkillsSection", self.sections_snippet)

    def test_build_providers_section_present(self):
        self.assertIn("buildProvidersSection", self.sections_snippet)

    def test_build_agents_section_present(self):
        self.assertIn("buildAgentsSection", self.sections_snippet)

    def test_build_mcp_servers_section_present(self):
        self.assertIn("buildMCPServersSection", self.sections_snippet)

    def test_build_lsp_section_present(self):
        self.assertIn("buildLSPSection", self.sections_snippet)

    def test_build_mesnada_section_present(self):
        self.assertIn("buildMesnadaSection", self.sections_snippet)

    def test_build_remembrances_section_present(self):
        self.assertIn("buildRemembrancesSection", self.sections_snippet)

    def test_build_internal_tools_section_present(self):
        self.assertIn("buildInternalToolsSection", self.sections_snippet)

    def test_build_server_section_present(self):
        self.assertIn("buildServerSection", self.sections_snippet)

    def test_build_lua_section_present(self):
        self.assertIn("buildLuaSection", self.sections_snippet)

    def test_build_mcp_gateway_section_present(self):
        self.assertIn("buildMCPGatewaySection", self.sections_snippet)

    def test_build_snapshots_section_present(self):
        self.assertIn("buildSnapshotsSection", self.sections_snippet)

    def test_build_evaluator_section_present(self):
        self.assertIn("buildEvaluatorSection", self.sections_snippet)

    # --- persistSetting coverage ---

    def test_persist_setting_handles_server_prefix(self):
        self.assertIn('"server."', self.persist_body)

    def test_persist_setting_handles_lua_prefix(self):
        self.assertIn('"lua."', self.persist_body)

    def test_persist_setting_handles_mcp_gateway_prefix(self):
        self.assertIn('"mcpGateway."', self.persist_body)

    def test_persist_setting_handles_snapshots_prefix(self):
        self.assertIn('"snapshots."', self.persist_body)

    def test_persist_setting_handles_evaluator_prefix(self):
        self.assertIn('"evaluator."', self.persist_body)

    def test_persist_setting_handles_general_prefix(self):
        self.assertIn('"general."', self.persist_body)


if __name__ == "__main__":
    unittest.main()
