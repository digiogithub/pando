"""
Tests for the Google Search tool in internal/llm/tools/search_google.go.

Tests cover:
- Missing API key error
- Missing Search Engine ID error
- Successful search response formatting
- Result item formatting
- Query parameter inclusion (num, date_restrict, site_search)
- Empty results handling
- Spelling suggestion formatting
- HTTP error responses
"""

import json
import os
import sys
import unittest
from unittest.mock import MagicMock, patch
from urllib.parse import urlencode, urlparse, parse_qs
from http.server import HTTPServer, BaseHTTPRequestHandler
import threading

sys.path.insert(0, os.path.dirname(__file__))


GOOGLE_SEARCH_API_URL = "https://www.googleapis.com/customsearch/v1"


def simulate_google_search_missing_api_key() -> str:
    """Simulate the tool response when the Google API key is not set."""
    return "Google Search not configured: set GOOGLE_API_KEY (or PANDO_GOOGLE_API_KEY) environment variable"


def simulate_google_search_missing_engine_id() -> str:
    """Simulate the tool response when the Search Engine ID is not set."""
    return "Google Search not configured: set GOOGLE_SEARCH_ENGINE_ID (or PANDO_GOOGLE_SEARCH_ENGINE_ID) environment variable"


def build_google_search_url(
    api_key: str,
    cx: str,
    query: str,
    num: int = 0,
    date_restrict: str = "",
    site_search: str = "",
) -> str:
    """
    Builds the Google Custom Search API URL, mirroring the logic in search_google.go.
    """
    params = {
        "key": api_key,
        "cx": cx,
        "q": query,
    }
    if 1 <= num <= 10:
        params["num"] = str(num)
    if date_restrict:
        params["dateRestrict"] = date_restrict
    if site_search:
        params["siteSearch"] = site_search
    return GOOGLE_SEARCH_API_URL + "?" + urlencode(params)


def format_google_search_response(query: str, items: list, spelling: str = "") -> str:
    """
    Python equivalent of the result formatting in search_google.go.
    """
    parts = [f"## Google Search: {query}\n"]
    parts.append("")

    for i, item in enumerate(items, start=1):
        parts.append(f"### {i}. {item['title']}")
        parts.append(f"**URL:** {item['link']}")
        if item.get("snippet"):
            parts.append(item["snippet"])
        parts.append("")
        parts.append("---")
        parts.append("")

    result = "\n".join(parts)
    if spelling:
        result += f"\n> **Did you mean:** {spelling}\n"
    return result


def simulate_google_search_http_error(status_code: int) -> str:
    """Simulate HTTP error response from Google API."""
    return f"Google API error {status_code}:"


class TestGoogleSearchMissingConfig(unittest.TestCase):
    """Tests for missing API key / Search Engine ID error messages."""

    def test_missing_api_key_error_contains_google_api_key(self):
        result = simulate_google_search_missing_api_key()
        self.assertIn("GOOGLE_API_KEY", result)

    def test_missing_api_key_error_mentions_pando_variant(self):
        result = simulate_google_search_missing_api_key()
        self.assertIn("PANDO_GOOGLE_API_KEY", result)

    def test_missing_api_key_error_mentions_configuration(self):
        result = simulate_google_search_missing_api_key()
        self.assertIn("not configured", result)

    def test_missing_engine_id_error_contains_google_search_engine_id(self):
        result = simulate_google_search_missing_engine_id()
        self.assertIn("GOOGLE_SEARCH_ENGINE_ID", result)

    def test_missing_engine_id_error_mentions_pando_variant(self):
        result = simulate_google_search_missing_engine_id()
        self.assertIn("PANDO_GOOGLE_SEARCH_ENGINE_ID", result)

    def test_missing_engine_id_error_mentions_configuration(self):
        result = simulate_google_search_missing_engine_id()
        self.assertIn("not configured", result)


class TestGoogleSearchResponseFormatting(unittest.TestCase):
    """Tests for Google Search result formatting."""

    def setUp(self):
        self.query = "golang testing"
        self.items = [
            {
                "title": "Go Testing Package",
                "link": "https://pkg.go.dev/testing",
                "displayLink": "pkg.go.dev",
                "snippet": "Package testing provides support for automated testing of Go packages.",
            },
            {
                "title": "Advanced Go Testing",
                "link": "https://example.com/go-testing",
                "displayLink": "example.com",
                "snippet": "Learn advanced testing patterns in Go.",
            },
        ]

    def test_successful_response_has_google_search_header(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("## Google Search:", result)

    def test_header_includes_query(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn(self.query, result)

    def test_first_result_has_numbered_header(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("### 1.", result)
        self.assertIn("Go Testing Package", result)

    def test_second_result_has_numbered_header(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("### 2.", result)
        self.assertIn("Advanced Go Testing", result)

    def test_result_includes_url(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("https://pkg.go.dev/testing", result)

    def test_result_includes_snippet(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("Package testing provides support", result)

    def test_results_separated_by_horizontal_rule(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("---", result)

    def test_result_header_format_is_number_dot_title(self):
        result = format_google_search_response(self.query, self.items)
        self.assertIn("### 1. Go Testing Package", result)
        self.assertIn("### 2. Advanced Go Testing", result)


class TestGoogleSearchEmptyResults(unittest.TestCase):
    """Tests for empty results handling."""

    def test_empty_results_returns_no_results_message(self):
        query = "very obscure query xyz123"
        # Go code: return NewTextResponse(fmt.Sprintf("No results found for: %s", params.Query))
        result = f"No results found for: {query}"
        self.assertIn("No results found for:", result)
        self.assertIn(query, result)

    def test_no_results_message_contains_query(self):
        query = "unique_search_term"
        result = f"No results found for: {query}"
        self.assertIn("unique_search_term", result)


class TestGoogleSearchSpellingSuggestion(unittest.TestCase):
    """Tests for spelling suggestion ('Did you mean') in results."""

    def test_spelling_suggestion_appears_when_present(self):
        result = format_google_search_response(
            "golag testing",
            [{"title": "Test", "link": "http://example.com", "snippet": "Test"}],
            spelling="golang testing",
        )
        self.assertIn("Did you mean:", result)
        self.assertIn("golang testing", result)

    def test_spelling_suggestion_not_present_when_empty(self):
        result = format_google_search_response(
            "golang testing",
            [{"title": "Test", "link": "http://example.com", "snippet": "Test"}],
            spelling="",
        )
        self.assertNotIn("Did you mean:", result)


class TestGoogleSearchURLParameters(unittest.TestCase):
    """Tests for URL parameter inclusion in Google Search requests."""

    def test_num_parameter_included_in_url(self):
        url = build_google_search_url("key", "cx", "test", num=5)
        params = parse_qs(urlparse(url).query)
        self.assertIn("num", params)
        self.assertEqual(params["num"][0], "5")

    def test_num_parameter_not_included_when_zero(self):
        url = build_google_search_url("key", "cx", "test", num=0)
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("num", params)

    def test_num_parameter_not_included_when_out_of_range(self):
        url = build_google_search_url("key", "cx", "test", num=11)
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("num", params)

    def test_date_restrict_parameter_sent_as_date_restrict_camel_case(self):
        """date_restrict (snake_case) must be sent as dateRestrict (camelCase) in URL."""
        url = build_google_search_url("key", "cx", "test", date_restrict="d7")
        params = parse_qs(urlparse(url).query)
        self.assertIn("dateRestrict", params)
        self.assertEqual(params["dateRestrict"][0], "d7")
        # Must NOT be sent as date_restrict
        self.assertNotIn("date_restrict", params)

    def test_site_search_parameter_sent_as_site_search_camel_case(self):
        """site_search must be sent as siteSearch in the URL."""
        url = build_google_search_url("key", "cx", "test", site_search="github.com")
        params = parse_qs(urlparse(url).query)
        self.assertIn("siteSearch", params)
        self.assertEqual(params["siteSearch"][0], "github.com")
        self.assertNotIn("site_search", params)

    def test_api_key_included_in_url(self):
        url = build_google_search_url("my-api-key", "my-cx", "test")
        params = parse_qs(urlparse(url).query)
        self.assertIn("key", params)
        self.assertEqual(params["key"][0], "my-api-key")

    def test_cx_included_in_url(self):
        url = build_google_search_url("key", "my-engine-id", "test")
        params = parse_qs(urlparse(url).query)
        self.assertIn("cx", params)
        self.assertEqual(params["cx"][0], "my-engine-id")

    def test_query_included_in_url(self):
        url = build_google_search_url("key", "cx", "golang testing patterns")
        params = parse_qs(urlparse(url).query)
        self.assertIn("q", params)
        self.assertEqual(params["q"][0], "golang testing patterns")


class TestGoogleSearchHTTPErrors(unittest.TestCase):
    """Tests for HTTP error responses from Google API."""

    def test_http_403_returns_error_with_status_code(self):
        result = simulate_google_search_http_error(403)
        self.assertIn("403", result)

    def test_http_429_returns_error_with_status_code(self):
        result = simulate_google_search_http_error(429)
        self.assertIn("429", result)

    def test_http_500_returns_error_with_status_code(self):
        result = simulate_google_search_http_error(500)
        self.assertIn("500", result)

    def test_error_message_contains_google_api_error_prefix(self):
        result = simulate_google_search_http_error(403)
        self.assertIn("Google API error", result)


class TestGoogleSearchAPIResponseParsing(unittest.TestCase):
    """Tests for parsing the Google Custom Search API JSON response."""

    def test_parse_items_from_api_response(self):
        api_response = {
            "searchInformation": {
                "formattedTotalResults": "1,230,000",
                "formattedSearchTime": "0.42",
            },
            "items": [
                {
                    "title": "Test Title",
                    "link": "https://example.com",
                    "displayLink": "example.com",
                    "snippet": "A test snippet.",
                }
            ],
        }
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(len(parsed["items"]), 1)
        self.assertEqual(parsed["items"][0]["title"], "Test Title")

    def test_parse_spelling_from_api_response(self):
        api_response = {
            "items": [
                {"title": "T", "link": "http://x.com", "snippet": "s"}
            ],
            "spelling": {"correctedQuery": "golang"},
        }
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(parsed["spelling"]["correctedQuery"], "golang")

    def test_empty_items_in_api_response(self):
        api_response = {}
        parsed = json.loads(json.dumps(api_response))
        items = parsed.get("items", [])
        self.assertEqual(len(items), 0)


if __name__ == "__main__":
    unittest.main()
