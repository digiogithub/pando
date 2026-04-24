"""Integration tests for LLM cache config API endpoint."""
import pytest
import requests

BASE_URL = "http://localhost:5555"


def test_get_settings_includes_llm_cache_enabled():
    """GET /api/v1/settings should return llm_cache_enabled field."""
    resp = requests.get(f"{BASE_URL}/api/v1/settings", timeout=5)
    if resp.status_code == 404:
        pytest.skip("Settings endpoint not available")
    assert resp.status_code == 200
    data = resp.json()
    assert "llm_cache_enabled" in data, "llm_cache_enabled missing from settings response"
    assert isinstance(data["llm_cache_enabled"], bool)


def test_put_settings_toggle_llm_cache():
    """PUT /api/v1/settings should accept llm_cache_enabled."""
    # Get current value
    resp = requests.get(f"{BASE_URL}/api/v1/settings", timeout=5)
    if resp.status_code == 404:
        pytest.skip("Settings endpoint not available")
    assert resp.status_code == 200
    original = resp.json().get("llm_cache_enabled", True)

    # Toggle it
    new_value = not original
    put_resp = requests.put(
        f"{BASE_URL}/api/v1/settings",
        json={"llm_cache_enabled": new_value},
        timeout=5,
    )
    assert put_resp.status_code == 200
    updated = put_resp.json()
    assert updated.get("llm_cache_enabled") == new_value

    # Restore original
    requests.put(
        f"{BASE_URL}/api/v1/settings",
        json={"llm_cache_enabled": original},
        timeout=5,
    )
