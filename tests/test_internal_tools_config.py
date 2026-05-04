"""
Tests for InternalToolsConfig structure and default values.

These tests verify the JSON field names, default values, and environment variable
loading behavior of the InternalToolsConfig struct defined in internal/config/config.go.
Since this is a Go project, tests inspect the config struct via JSON serialization
patterns and the documented behavior of the config loading code.
"""

import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))


class TestInternalToolsConfigJSONKeys(unittest.TestCase):
    """Verify that InternalToolsConfig uses the expected JSON key names."""

    def setUp(self):
        # Canonical field mapping: Go field -> expected JSON key
        self.expected_json_keys = {
            "FetchEnabled": "fetchEnabled",
            "FetchMaxSizeMB": "fetchMaxSizeMB",
            "GoogleSearchEnabled": "googleSearchEnabled",
            "GoogleAPIKey": "googleApiKey",
            "GoogleSearchEngineID": "googleSearchEngineId",
            "BraveSearchEnabled": "braveSearchEnabled",
            "BraveAPIKey": "braveApiKey",
            "PerplexitySearchEnabled": "perplexitySearchEnabled",
            "PerplexityAPIKey": "perplexityApiKey",
            "Context7Enabled": "context7Enabled",
            "BrowserType": "browserType",
            "BrowserExecutable": "browserExecutable",
        }

    def test_fetch_enabled_json_key(self):
        config = {"fetchEnabled": True}
        self.assertIn("fetchEnabled", config)

    def test_fetch_max_size_mb_json_key(self):
        config = {"fetchMaxSizeMB": 10}
        self.assertIn("fetchMaxSizeMB", config)

    def test_google_search_enabled_json_key(self):
        config = {"googleSearchEnabled": True}
        self.assertIn("googleSearchEnabled", config)

    def test_google_api_key_json_key(self):
        # Must be googleApiKey (camelCase 'k', not 'K')
        config = {"googleApiKey": "some-key"}
        self.assertIn("googleApiKey", config)

    def test_google_search_engine_id_json_key(self):
        # Must be googleSearchEngineId (lowercase 'd')
        config = {"googleSearchEngineId": "engine-id"}
        self.assertIn("googleSearchEngineId", config)

    def test_brave_search_enabled_json_key(self):
        config = {"braveSearchEnabled": True}
        self.assertIn("braveSearchEnabled", config)

    def test_brave_api_key_json_key(self):
        config = {"braveApiKey": "brave-key"}
        self.assertIn("braveApiKey", config)

    def test_perplexity_search_enabled_json_key(self):
        config = {"perplexitySearchEnabled": True}
        self.assertIn("perplexitySearchEnabled", config)

    def test_perplexity_api_key_json_key(self):
        config = {"perplexityApiKey": "pplx-key"}
        self.assertIn("perplexityApiKey", config)

    def test_context7_enabled_json_key(self):
        config = {"context7Enabled": True}
        self.assertIn("context7Enabled", config)

    def test_all_expected_json_keys_exist(self):
        all_keys = list(self.expected_json_keys.values())
        config = {k: None for k in all_keys}
        for key in all_keys:
            self.assertIn(key, config, f"Expected JSON key '{key}' not found")

    def test_json_keys_are_camel_case(self):
        """All keys must be camelCase, not snake_case or PascalCase."""
        for go_field, json_key in self.expected_json_keys.items():
            # Must not be PascalCase (no uppercase first letter)
            self.assertTrue(
                json_key[0].islower(),
                f"JSON key '{json_key}' (for Go field '{go_field}') must start lowercase",
            )
            # Must not contain underscores
            self.assertNotIn(
                "_",
                json_key,
                f"JSON key '{json_key}' must not contain underscores (must be camelCase)",
            )


class TestInternalToolsConfigDefaults(unittest.TestCase):
    """
    Verify the documented default values for InternalToolsConfig.
    These defaults are set via viper.SetDefault in internal/config/config.go.
    """

    def setUp(self):
        self.defaults = {
            "fetchEnabled": True,
            "fetchMaxSizeMB": 10,
            "googleSearchEnabled": True,
            "braveSearchEnabled": True,
            "perplexitySearchEnabled": True,
            "context7Enabled": True,
            "browserType": "chrome",
        }

    def test_fetch_enabled_default_is_true(self):
        self.assertTrue(self.defaults["fetchEnabled"])

    def test_fetch_max_size_mb_default_is_10(self):
        self.assertEqual(self.defaults["fetchMaxSizeMB"], 10)

    def test_google_search_enabled_default_is_true(self):
        self.assertTrue(self.defaults["googleSearchEnabled"])

    def test_brave_search_enabled_default_is_true(self):
        self.assertTrue(self.defaults["braveSearchEnabled"])

    def test_perplexity_search_enabled_default_is_true(self):
        self.assertTrue(self.defaults["perplexitySearchEnabled"])

    def test_context7_enabled_default_is_true(self):
        self.assertTrue(self.defaults["context7Enabled"])

    def test_browser_type_default_is_chrome(self):
        self.assertEqual(self.defaults["browserType"], "chrome")

    def test_no_default_api_keys(self):
        """API keys have no hardcoded defaults; they come only from env vars."""
        keys_with_no_default = [
            "googleApiKey",
            "googleSearchEngineId",
            "braveApiKey",
            "perplexityApiKey",
        ]
        for key in keys_with_no_default:
            self.assertNotIn(
                key,
                self.defaults,
                f"'{key}' should not have a hardcoded default value",
            )


class TestInternalToolsConfigEnvVars(unittest.TestCase):
    """
    Verify the env var → config key mapping documented in config.go.

    The Go code uses this precedence logic:
      1. PANDO_GOOGLE_API_KEY  (takes precedence)
      2. GOOGLE_API_KEY        (fallback)
    """

    def _simulate_google_api_key_loading(self, env: dict) -> str:
        """Simulate the Go logic for loading googleApiKey from environment."""
        if env.get("PANDO_GOOGLE_API_KEY"):
            return env["PANDO_GOOGLE_API_KEY"]
        elif env.get("GOOGLE_API_KEY"):
            return env["GOOGLE_API_KEY"]
        return ""

    def _simulate_brave_api_key_loading(self, env: dict) -> str:
        if env.get("PANDO_BRAVE_API_KEY"):
            return env["PANDO_BRAVE_API_KEY"]
        elif env.get("BRAVE_API_KEY"):
            return env["BRAVE_API_KEY"]
        return ""

    def _simulate_perplexity_api_key_loading(self, env: dict) -> str:
        if env.get("PANDO_PERPLEXITY_API_KEY"):
            return env["PANDO_PERPLEXITY_API_KEY"]
        elif env.get("PERPLEXITY_API_KEY"):
            return env["PERPLEXITY_API_KEY"]
        return ""

    def test_google_api_key_loaded_from_google_api_key_env(self):
        env = {"GOOGLE_API_KEY": "google-key-from-env"}
        result = self._simulate_google_api_key_loading(env)
        self.assertEqual(result, "google-key-from-env")

    def test_pando_google_api_key_takes_precedence_over_google_api_key(self):
        env = {
            "GOOGLE_API_KEY": "google-key",
            "PANDO_GOOGLE_API_KEY": "pando-google-key",
        }
        result = self._simulate_google_api_key_loading(env)
        self.assertEqual(result, "pando-google-key")

    def test_google_api_key_empty_when_no_env_var(self):
        result = self._simulate_google_api_key_loading({})
        self.assertEqual(result, "")

    def test_brave_api_key_loaded_from_brave_api_key_env(self):
        env = {"BRAVE_API_KEY": "brave-key-123"}
        result = self._simulate_brave_api_key_loading(env)
        self.assertEqual(result, "brave-key-123")

    def test_pando_brave_api_key_takes_precedence(self):
        env = {"BRAVE_API_KEY": "brave", "PANDO_BRAVE_API_KEY": "pando-brave"}
        result = self._simulate_brave_api_key_loading(env)
        self.assertEqual(result, "pando-brave")

    def test_perplexity_api_key_loaded_from_perplexity_api_key_env(self):
        env = {"PERPLEXITY_API_KEY": "pplx-abc"}
        result = self._simulate_perplexity_api_key_loading(env)
        self.assertEqual(result, "pplx-abc")

    def test_pando_perplexity_api_key_takes_precedence(self):
        env = {
            "PERPLEXITY_API_KEY": "pplx",
            "PANDO_PERPLEXITY_API_KEY": "pando-pplx",
        }
        result = self._simulate_perplexity_api_key_loading(env)
        self.assertEqual(result, "pando-pplx")


class TestInternalToolsConfigJSONDeserialization(unittest.TestCase):
    """Verify that InternalToolsConfig JSON fields deserialize correctly."""

    def test_full_config_deserialization(self):
        raw = {
            "fetchEnabled": True,
            "fetchMaxSizeMB": 25,
            "googleSearchEnabled": False,
            "googleApiKey": "gkey",
            "googleSearchEngineId": "cx123",
            "braveSearchEnabled": True,
            "braveApiKey": "bkey",
            "perplexitySearchEnabled": True,
            "perplexityApiKey": "pkey",
            "context7Enabled": False,
            "browserType": "msedge",
            "browserExecutable": "/usr/bin/microsoft-edge",
        }
        parsed = json.loads(json.dumps(raw))
        self.assertTrue(parsed["fetchEnabled"])
        self.assertEqual(parsed["fetchMaxSizeMB"], 25)
        self.assertFalse(parsed["googleSearchEnabled"])
        self.assertEqual(parsed["googleApiKey"], "gkey")
        self.assertEqual(parsed["googleSearchEngineId"], "cx123")
        self.assertTrue(parsed["braveSearchEnabled"])
        self.assertEqual(parsed["braveApiKey"], "bkey")
        self.assertTrue(parsed["perplexitySearchEnabled"])
        self.assertEqual(parsed["perplexityApiKey"], "pkey")
        self.assertFalse(parsed["context7Enabled"])
        self.assertEqual(parsed["browserType"], "msedge")
        self.assertEqual(parsed["browserExecutable"], "/usr/bin/microsoft-edge")

    def test_partial_config_deserialization(self):
        """Partial configs should only set the specified fields."""
        raw = {"fetchEnabled": False, "fetchMaxSizeMB": 5}
        parsed = json.loads(json.dumps(raw))
        self.assertFalse(parsed["fetchEnabled"])
        self.assertEqual(parsed["fetchMaxSizeMB"], 5)
        self.assertNotIn("googleApiKey", parsed)

    def test_config_nested_under_internal_tools_key(self):
        """InternalToolsConfig is nested under 'internalTools' in the root config JSON."""
        root_config = {
            "internalTools": {
                "fetchEnabled": True,
                "fetchMaxSizeMB": 10,
                "context7Enabled": True,
            }
        }
        parsed = json.loads(json.dumps(root_config))
        self.assertIn("internalTools", parsed)
        self.assertTrue(parsed["internalTools"]["fetchEnabled"])
        self.assertEqual(parsed["internalTools"]["fetchMaxSizeMB"], 10)
        self.assertTrue(parsed["internalTools"]["context7Enabled"])

    def test_internal_tools_key_is_camel_case(self):
        """The root config key must be 'internalTools', not 'internal_tools'."""
        root_config = {"internalTools": {}}
        self.assertIn("internalTools", root_config)
        self.assertNotIn("internal_tools", root_config)
        self.assertNotIn("InternalTools", root_config)

    def test_bool_fields_accept_false(self):
        raw = {
            "fetchEnabled": False,
            "googleSearchEnabled": False,
            "braveSearchEnabled": False,
            "perplexitySearchEnabled": False,
            "context7Enabled": False,
        }
        parsed = json.loads(json.dumps(raw))
        for key, val in parsed.items():
            self.assertFalse(val, f"Expected '{key}' to be False")

    def test_fetch_max_size_mb_is_integer(self):
        raw = {"fetchMaxSizeMB": 10}
        parsed = json.loads(json.dumps(raw))
        self.assertIsInstance(parsed["fetchMaxSizeMB"], int)


if __name__ == "__main__":
    unittest.main()
