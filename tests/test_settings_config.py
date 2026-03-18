"""
Integration tests for the Settings TUI — Full Config Coverage.

Verifies that:
- The project builds successfully (all 14 sections wired in buildSections)
- The config package tests pass
- The new Update* functions and TUI sections integrate correctly
"""

import subprocess
import pytest

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


def test_settings_page_compiles():
    """Verify the TUI settings page package compiles cleanly (all 14 sections present)."""
    result = subprocess.run(
        ["go", "build", "./internal/tui/page/..."],
        cwd=PANDO_ROOT,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, (
        f"TUI settings page build failed:\n{result.stderr}"
    )


def test_all_14_sections_referenced():
    """
    Verify that buildSections() references all 14 expected build* functions.
    Reads settings.go and confirms each function call is present in buildSections.
    """
    settings_path = f"{PANDO_ROOT}/internal/tui/page/settings.go"

    expected_sections = [
        "buildGeneralSection",
        "buildSkillsSection",
        "buildProvidersSection",
        "buildAgentsSection",
        "buildMCPServersSection",
        "buildLSPSection",
        "buildMesnadaSection",
        "buildRemembrancesSection",
        "buildInternalToolsSection",
        "buildServerSection",
        "buildLuaSection",
        "buildMCPGatewaySection",
        "buildSnapshotsSection",
        "buildEvaluatorSection",
    ]

    with open(settings_path, "r") as f:
        source = f.read()

    # Find the buildSections function body
    start = source.find("func buildSections(")
    assert start != -1, "buildSections function not found in settings.go"
    # Take a generous slice after the function start to cover the return statement
    snippet = source[start : start + 1000]

    for section_func in expected_sections:
        assert section_func in snippet, (
            f"{section_func} is missing from buildSections() in settings.go"
        )


def test_persist_setting_covers_new_keys():
    """
    Verify that persistSetting() handles all new key prefixes introduced in Phases 3-4.
    """
    settings_path = f"{PANDO_ROOT}/internal/tui/page/settings.go"

    required_cases = [
        '"server."',
        '"lua."',
        '"mcpGateway."',
        '"snapshots."',
        '"evaluator."',
        '"general."',
    ]

    with open(settings_path, "r") as f:
        source = f.read()

    persist_start = source.find("func persistSetting(")
    assert persist_start != -1, "persistSetting function not found"
    persist_end = source.find("\nfunc ", persist_start + 1)
    persist_body = source[persist_start:persist_end]

    for case in required_cases:
        assert case in persist_body, (
            f"persistSetting() is missing case for {case}"
        )
