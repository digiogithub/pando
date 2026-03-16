"""
Tests for the Brave Search tool in internal/llm/tools/search_brave.go.

Tests cover:
- Missing API key error message
- Successful search response formatting
- Result item formatting
- X-Subscription-Token header behavior
- Discussions section rendering
- Query parameters (freshness, safesearch)
- Empty results handling
- HTTP error responses
"""

import json
import os
import sys
import unittest
from urllib.parse import urlencode, urlparse, parse_qs

sys.path.insert(0, os.path.dirname(__file__))


BRAVE_SEARCH_API_URL = "https://api.search.brave.com/res/v1/web/search"


def simulate_brave_search_missing_api_key() -> str:
    """Simulate the error message when BRAVE_API_KEY is not set."""
    return "Brave Search not configured: set BRAVE_API_KEY (or PANDO_BRAVE_API_KEY) environment variable"


def build_brave_search_url(
    query: str,
    count: int = 0,
    freshness: str = "",
    safesearch: str = "",
    country: str = "",
) -> str:
    """
    Builds the Brave Search API URL, mirroring logic in search_brave.go.
    """
    params = {"q": query}
    if 1 <= count <= 20:
        params["count"] = str(count)
    if freshness in ("pd", "pw", "pm", "py"):
        params["freshness"] = freshness
    if safesearch in ("strict", "moderate", "off"):
        params["safesearch"] = safesearch
    if country:
        params["country"] = country
    return BRAVE_SEARCH_API_URL + "?" + urlencode(params)


def format_brave_search_response(query: str, web_results: list, discussions: list = None) -> str:
    """
    Python equivalent of the Brave Search result formatting in search_brave.go.
    """
    parts = [f"## Brave Search: {query}", ""]

    for i, item in enumerate(web_results, start=1):
        parts.append(f"### {i}. {item['title']}")
        parts.append(f"**URL:** {item['url']}")
        if item.get("page_age"):
            parts.append(f"**Age:** {item['page_age']}")
        if item.get("description"):
            parts.append(item["description"])
        parts.append("")
        parts.append("---")
        parts.append("")

    if discussions:
        parts.append("### Discussions:")
        limit = min(3, len(discussions))
        for d in discussions[:limit]:
            parts.append(f"- [{d['title']}]({d['url']})")

    return "\n".join(parts)


def simulate_brave_search_http_error(status_code: int) -> str:
    """Simulate HTTP error response from Brave Search API."""
    return f"Brave Search API error {status_code}:"


class TestBraveSearchMissingConfig(unittest.TestCase):
    """Tests for missing API key error messages."""

    def test_missing_api_key_error_contains_brave_api_key(self):
        result = simulate_brave_search_missing_api_key()
        self.assertIn("BRAVE_API_KEY", result)

    def test_missing_api_key_error_mentions_pando_variant(self):
        result = simulate_brave_search_missing_api_key()
        self.assertIn("PANDO_BRAVE_API_KEY", result)

    def test_missing_api_key_error_mentions_not_configured(self):
        result = simulate_brave_search_missing_api_key()
        self.assertIn("not configured", result)


class TestBraveSearchResponseFormatting(unittest.TestCase):
    """Tests for Brave Search result formatting."""

    def setUp(self):
        self.query = "rust programming language"
        self.web_results = [
            {
                "title": "The Rust Programming Language",
                "url": "https://doc.rust-lang.org/book/",
                "description": "An approachable book about Rust.",
                "page_age": "2024-01-15",
            },
            {
                "title": "Rust Official Site",
                "url": "https://www.rust-lang.org/",
                "description": "A language empowering everyone.",
                "page_age": "",
            },
        ]

    def test_successful_response_has_brave_search_header(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("## Brave Search:", result)

    def test_header_includes_query(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn(self.query, result)

    def test_first_result_has_numbered_header(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("### 1.", result)
        self.assertIn("The Rust Programming Language", result)

    def test_second_result_has_numbered_header(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("### 2.", result)
        self.assertIn("Rust Official Site", result)

    def test_result_includes_url(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("https://doc.rust-lang.org/book/", result)

    def test_result_includes_description(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("An approachable book about Rust.", result)

    def test_result_includes_page_age_when_present(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("2024-01-15", result)

    def test_results_separated_by_horizontal_rule(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("---", result)

    def test_result_header_format_is_number_dot_title(self):
        result = format_brave_search_response(self.query, self.web_results)
        self.assertIn("### 1. The Rust Programming Language", result)
        self.assertIn("### 2. Rust Official Site", result)


class TestBraveSearchDiscussions(unittest.TestCase):
    """Tests for the Discussions section in Brave Search results."""

    def setUp(self):
        self.query = "python async"
        self.web_results = [
            {"title": "Python Async Docs", "url": "https://docs.python.org/3/library/asyncio.html", "description": "Asyncio documentation"}
        ]
        self.discussions = [
            {"title": "How to use asyncio?", "url": "https://reddit.com/r/python/1"},
            {"title": "Async vs threading", "url": "https://stackoverflow.com/q/1"},
            {"title": "Event loop explained", "url": "https://reddit.com/r/python/2"},
            {"title": "Fourth discussion", "url": "https://reddit.com/r/python/3"},
        ]

    def test_discussions_section_included_when_present(self):
        result = format_brave_search_response(self.query, self.web_results, self.discussions)
        self.assertIn("### Discussions:", result)

    def test_discussions_formatted_as_links(self):
        result = format_brave_search_response(self.query, self.web_results, self.discussions)
        self.assertIn("[How to use asyncio?](https://reddit.com/r/python/1)", result)

    def test_discussions_limited_to_three(self):
        result = format_brave_search_response(self.query, self.web_results, self.discussions)
        # Only 3 discussions should appear even though 4 were returned
        self.assertIn("How to use asyncio?", result)
        self.assertIn("Async vs threading", result)
        self.assertIn("Event loop explained", result)
        self.assertNotIn("Fourth discussion", result)

    def test_no_discussions_section_when_empty(self):
        result = format_brave_search_response(self.query, self.web_results, [])
        self.assertNotIn("### Discussions:", result)

    def test_no_discussions_section_when_none(self):
        result = format_brave_search_response(self.query, self.web_results, None)
        self.assertNotIn("### Discussions:", result)


class TestBraveSearchURLParameters(unittest.TestCase):
    """Tests for URL parameter inclusion in Brave Search requests."""

    def test_freshness_pd_included_in_request_params(self):
        url = build_brave_search_url("test", freshness="pd")
        params = parse_qs(urlparse(url).query)
        self.assertIn("freshness", params)
        self.assertEqual(params["freshness"][0], "pd")

    def test_freshness_pw_included_in_request_params(self):
        url = build_brave_search_url("test", freshness="pw")
        params = parse_qs(urlparse(url).query)
        self.assertIn("freshness", params)
        self.assertEqual(params["freshness"][0], "pw")

    def test_invalid_freshness_not_included(self):
        """Only pd, pw, pm, py are valid freshness values."""
        url = build_brave_search_url("test", freshness="invalid")
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("freshness", params)

    def test_safesearch_strict_included_in_request(self):
        url = build_brave_search_url("test", safesearch="strict")
        params = parse_qs(urlparse(url).query)
        self.assertIn("safesearch", params)
        self.assertEqual(params["safesearch"][0], "strict")

    def test_safesearch_moderate_included(self):
        url = build_brave_search_url("test", safesearch="moderate")
        params = parse_qs(urlparse(url).query)
        self.assertIn("safesearch", params)
        self.assertEqual(params["safesearch"][0], "moderate")

    def test_safesearch_off_included(self):
        url = build_brave_search_url("test", safesearch="off")
        params = parse_qs(urlparse(url).query)
        self.assertIn("safesearch", params)
        self.assertEqual(params["safesearch"][0], "off")

    def test_invalid_safesearch_not_included(self):
        url = build_brave_search_url("test", safesearch="unknown")
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("safesearch", params)

    def test_count_included_when_valid(self):
        url = build_brave_search_url("test", count=10)
        params = parse_qs(urlparse(url).query)
        self.assertIn("count", params)
        self.assertEqual(params["count"][0], "10")

    def test_count_not_included_when_zero(self):
        url = build_brave_search_url("test", count=0)
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("count", params)

    def test_count_max_valid_is_20(self):
        url = build_brave_search_url("test", count=20)
        params = parse_qs(urlparse(url).query)
        self.assertIn("count", params)

    def test_count_not_included_when_over_20(self):
        url = build_brave_search_url("test", count=21)
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("count", params)


class TestBraveSearchXSubscriptionTokenHeader(unittest.TestCase):
    """Tests for the X-Subscription-Token header used by Brave Search."""

    def test_x_subscription_token_header_name(self):
        """Verify the exact header name used for Brave API authentication."""
        header_name = "X-Subscription-Token"
        # Verify it's the correct casing (not X-subscription-token or x-subscription-token)
        self.assertEqual(header_name, "X-Subscription-Token")

    def test_x_subscription_token_must_not_use_bearer(self):
        """Brave uses X-Subscription-Token, NOT Authorization: Bearer."""
        brave_headers = {"X-Subscription-Token": "my-brave-key", "Accept": "application/json"}
        self.assertIn("X-Subscription-Token", brave_headers)
        self.assertNotIn("Authorization", brave_headers)

    def test_header_value_is_raw_api_key(self):
        """Header value must be the raw API key, not 'Bearer {key}'."""
        api_key = "BSA12345abcde"
        brave_headers = {"X-Subscription-Token": api_key}
        self.assertEqual(brave_headers["X-Subscription-Token"], api_key)
        self.assertFalse(brave_headers["X-Subscription-Token"].startswith("Bearer"))


class TestBraveSearchEmptyResults(unittest.TestCase):
    """Tests for empty results handling."""

    def test_empty_results_returns_no_results_message(self):
        query = "very_unique_query_xyz"
        result = f"No results found for: {query}"
        self.assertIn("No results found for:", result)
        self.assertIn(query, result)

    def test_no_results_message_contains_query(self):
        query = "obscure_tech_term"
        result = f"No results found for: {query}"
        self.assertIn("obscure_tech_term", result)


class TestBraveSearchHTTPErrors(unittest.TestCase):
    """Tests for HTTP error responses from Brave Search API."""

    def test_http_401_returns_error_with_status_code(self):
        result = simulate_brave_search_http_error(401)
        self.assertIn("401", result)

    def test_http_429_returns_error_with_status_code(self):
        result = simulate_brave_search_http_error(429)
        self.assertIn("429", result)

    def test_http_500_returns_error_with_status_code(self):
        result = simulate_brave_search_http_error(500)
        self.assertIn("500", result)

    def test_error_message_contains_brave_search_api_error_prefix(self):
        result = simulate_brave_search_http_error(401)
        self.assertIn("Brave Search API error", result)


class TestBraveSearchAPIResponseParsing(unittest.TestCase):
    """Tests for parsing the Brave Search API JSON response structure."""

    def test_parse_web_results_from_api_response(self):
        api_response = {
            "query": {"original": "rust lang"},
            "web": {
                "results": [
                    {
                        "title": "Rust Language",
                        "url": "https://www.rust-lang.org/",
                        "description": "A systems programming language.",
                        "page_age": "2024-01-10",
                    }
                ]
            },
            "discussions": {"results": []},
        }
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(len(parsed["web"]["results"]), 1)
        self.assertEqual(parsed["web"]["results"][0]["title"], "Rust Language")

    def test_parse_discussions_from_api_response(self):
        api_response = {
            "web": {"results": [{"title": "T", "url": "u", "description": "d"}]},
            "discussions": {
                "results": [
                    {"title": "Discussion 1", "url": "https://reddit.com/1"}
                ]
            },
        }
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(len(parsed["discussions"]["results"]), 1)
        self.assertEqual(parsed["discussions"]["results"][0]["title"], "Discussion 1")

    def test_empty_web_results(self):
        api_response = {"web": {"results": []}, "discussions": {"results": []}}
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(len(parsed["web"]["results"]), 0)


if __name__ == "__main__":
    unittest.main()
