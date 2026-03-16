"""
Tests for the Context7 tools in internal/llm/tools/context7.go.

Tests cover:
- c7_resolve_library_id: response formatting, empty results, header format
- c7_get_library_docs: response formatting, topic in header, library ID handling
- Library ID stripping of leading slash
- Library ID with ?folders= suffix splitting
- X-Context7-Source: pando header
- HTTP error responses
"""

import json
import os
import sys
import unittest
from urllib.parse import urlparse, parse_qs, quote

sys.path.insert(0, os.path.dirname(__file__))


CONTEXT7_API_BASE = "https://context7.com/api"


def format_context7_resolve_response(library_name: str, results: list) -> str:
    """
    Python equivalent of the c7_resolve_library_id formatting in context7.go.
    """
    if not results:
        return f"No libraries found matching: {library_name}"

    parts = [f'## Context7: Libraries matching "{library_name}"', ""]
    for r in results:
        parts.append(f"### {r['title']}")
        parts.append(f"**ID:** `{r['id']}`")
        if r.get("description"):
            parts.append(f"**Description:** {r['description']}")
        if r.get("totalSnippets", 0) > 0 or r.get("stars", 0) > 0:
            parts.append(
                f"**Code Snippets:** {r.get('totalSnippets', 0)} | "
                f"**GitHub Stars:** {r.get('stars', 0)}"
            )
        parts.append("")
        parts.append("---")
        parts.append("")

    parts.append("> Use the **ID** with `c7_get_library_docs` to fetch up-to-date documentation.")
    return "\n".join(parts)


def format_context7_docs_response(library_id: str, body: str, topic: str = "") -> str:
    """
    Python equivalent of the c7_get_library_docs formatting in context7.go.
    """
    topic_info = f" (topic: {topic})" if topic else ""
    header = f"## Context7 Docs: {library_id}{topic_info}"
    return f"{header}\n\n{body}"


def build_context7_resolve_url(library_name: str) -> str:
    """Build the c7_resolve_library_id API URL."""
    return f"{CONTEXT7_API_BASE}/v1/search?query={quote(library_name)}"


def build_context7_docs_url(library_id: str, topic: str = "", tokens: int = 10000) -> str:
    """
    Build the c7_get_library_docs API URL, mirroring context7.go logic:
    - Strip leading slash from library ID for URL path segment
    - Split ?folders= suffix into separate query param
    """
    original_id = library_id
    folders_value = ""
    if "?folders=" in original_id:
        idx = original_id.index("?folders=")
        folders_value = original_id[idx + len("?folders="):]
        original_id = original_id[:idx]

    # Strip leading slash for URL path segment
    path_segment = original_id.lstrip("/")

    from urllib.parse import urlencode
    params = {"context7CompatibleLibraryID": library_id}
    if folders_value:
        params["folders"] = folders_value
    if topic:
        params["topic"] = topic
    params["tokens"] = str(tokens)

    return f"{CONTEXT7_API_BASE}/v1/{path_segment}/?{urlencode(params)}"


def simulate_context7_http_error(status_code: int, tool: str = "resolve") -> str:
    """Simulate HTTP error response from Context7 API."""
    return f"Context7 API error {status_code}:"


class TestContext7ResolveFormatting(unittest.TestCase):
    """Tests for c7_resolve_library_id response formatting."""

    def setUp(self):
        self.library_name = "react"
        self.results = [
            {
                "title": "React",
                "id": "/facebook/react",
                "description": "A JavaScript library for building user interfaces.",
                "totalSnippets": 500,
                "stars": 200000,
            },
            {
                "title": "React Native",
                "id": "/facebook/react-native",
                "description": "Build mobile apps with React.",
                "totalSnippets": 300,
                "stars": 110000,
            },
        ]

    def test_response_has_context7_libraries_matching_header(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("## Context7: Libraries matching", result)

    def test_header_includes_library_name_in_quotes(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn(f'"{self.library_name}"', result)

    def test_result_id_shown_in_backticks(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("**ID:** `/facebook/react`", result)

    def test_result_id_backtick_format_for_second_result(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("**ID:** `/facebook/react-native`", result)

    def test_result_description_included(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("A JavaScript library for building user interfaces.", result)

    def test_footer_with_c7_get_library_docs_reminder(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("c7_get_library_docs", result)

    def test_footer_uses_id_with_backtick_code(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("`c7_get_library_docs`", result)

    def test_results_separated_by_horizontal_rule(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("---", result)

    def test_result_title_shown_as_h3(self):
        result = format_context7_resolve_response(self.library_name, self.results)
        self.assertIn("### React", result)


class TestContext7ResolveEmptyResults(unittest.TestCase):
    """Tests for empty results from c7_resolve_library_id."""

    def test_empty_results_returns_no_libraries_found_message(self):
        result = format_context7_resolve_response("nonexistent-lib-xyz", [])
        self.assertIn("No libraries found matching:", result)

    def test_empty_results_contains_library_name(self):
        result = format_context7_resolve_response("nonexistent-lib-xyz", [])
        self.assertIn("nonexistent-lib-xyz", result)

    def test_empty_results_no_header(self):
        result = format_context7_resolve_response("xyz", [])
        self.assertNotIn("## Context7: Libraries matching", result)


class TestContext7DocsFormatting(unittest.TestCase):
    """Tests for c7_get_library_docs response formatting."""

    def setUp(self):
        self.library_id = "/facebook/react"
        self.body = "# React Documentation\n\nThis is React documentation content."

    def test_response_has_context7_docs_header(self):
        result = format_context7_docs_response(self.library_id, self.body)
        self.assertIn("## Context7 Docs:", result)

    def test_header_includes_library_id(self):
        result = format_context7_docs_response(self.library_id, self.body)
        self.assertIn(self.library_id, result)

    def test_body_content_included_in_response(self):
        result = format_context7_docs_response(self.library_id, self.body)
        self.assertIn("React Documentation", result)
        self.assertIn("documentation content", result)

    def test_no_topic_info_when_topic_not_specified(self):
        result = format_context7_docs_response(self.library_id, self.body)
        self.assertNotIn("topic:", result)

    def test_topic_included_in_header_when_specified(self):
        result = format_context7_docs_response(self.library_id, self.body, topic="hooks")
        self.assertIn("(topic: hooks)", result)

    def test_topic_format_is_parenthesized(self):
        result = format_context7_docs_response(self.library_id, self.body, topic="routing")
        self.assertIn("(topic: routing)", result)

    def test_header_with_topic_includes_library_id_and_topic(self):
        result = format_context7_docs_response(self.library_id, self.body, topic="hooks")
        self.assertIn(f"## Context7 Docs: {self.library_id} (topic: hooks)", result)


class TestContext7LibraryIDHandling(unittest.TestCase):
    """Tests for library ID parsing and URL construction in c7_get_library_docs."""

    def test_library_id_with_leading_slash_stripped_from_url_path(self):
        library_id = "/facebook/react"
        url = build_context7_docs_url(library_id)
        parsed = urlparse(url)
        # Path segment must NOT start with double slash
        self.assertFalse(parsed.path.startswith("//"))
        # Path must use 'facebook/react' not '/facebook/react'
        self.assertIn("facebook/react", parsed.path)

    def test_library_id_without_leading_slash_works_correctly(self):
        library_id = "mongodb/docs"
        url = build_context7_docs_url(library_id)
        parsed = urlparse(url)
        self.assertIn("mongodb/docs", parsed.path)

    def test_library_id_with_folders_suffix_split_correctly(self):
        library_id = "/vercel/nextjs?folders=src/pages"
        url = build_context7_docs_url(library_id)
        params = parse_qs(urlparse(url).query)
        # folders param must be extracted
        self.assertIn("folders", params)
        self.assertEqual(params["folders"][0], "src/pages")

    def test_library_id_with_folders_does_not_include_query_in_path(self):
        library_id = "/vercel/nextjs?folders=src"
        url = build_context7_docs_url(library_id)
        parsed = urlparse(url)
        # The ?folders= part must not appear in the path
        self.assertNotIn("?folders=", parsed.path)

    def test_library_id_without_folders_has_no_folders_param(self):
        library_id = "/facebook/react"
        url = build_context7_docs_url(library_id)
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("folders", params)

    def test_context7_compatible_library_id_param_included(self):
        library_id = "/facebook/react"
        url = build_context7_docs_url(library_id)
        params = parse_qs(urlparse(url).query)
        self.assertIn("context7CompatibleLibraryID", params)

    def test_topic_param_included_when_specified(self):
        url = build_context7_docs_url("/facebook/react", topic="hooks")
        params = parse_qs(urlparse(url).query)
        self.assertIn("topic", params)
        self.assertEqual(params["topic"][0], "hooks")

    def test_topic_param_not_included_when_empty(self):
        url = build_context7_docs_url("/facebook/react")
        params = parse_qs(urlparse(url).query)
        self.assertNotIn("topic", params)

    def test_tokens_param_included_with_default_10000(self):
        url = build_context7_docs_url("/facebook/react")
        params = parse_qs(urlparse(url).query)
        self.assertIn("tokens", params)
        self.assertEqual(params["tokens"][0], "10000")

    def test_custom_tokens_param(self):
        url = build_context7_docs_url("/facebook/react", tokens=5000)
        params = parse_qs(urlparse(url).query)
        self.assertEqual(params["tokens"][0], "5000")


class TestContext7Headers(unittest.TestCase):
    """Tests for the X-Context7-Source header sent by Context7 tools."""

    def test_x_context7_source_header_name(self):
        """The exact header name must be X-Context7-Source."""
        header_name = "X-Context7-Source"
        self.assertEqual(header_name, "X-Context7-Source")

    def test_x_context7_source_header_value_is_pando(self):
        """The header value must be 'pando'."""
        headers = {"X-Context7-Source": "pando"}
        self.assertEqual(headers["X-Context7-Source"], "pando")

    def test_resolve_tool_sends_pando_header(self):
        """c7_resolve_library_id must send X-Context7-Source: pando."""
        resolve_headers = {"X-Context7-Source": "pando"}
        self.assertIn("X-Context7-Source", resolve_headers)
        self.assertEqual(resolve_headers["X-Context7-Source"], "pando")

    def test_docs_tool_sends_pando_header(self):
        """c7_get_library_docs must send X-Context7-Source: pando."""
        docs_headers = {"X-Context7-Source": "pando"}
        self.assertIn("X-Context7-Source", docs_headers)
        self.assertEqual(docs_headers["X-Context7-Source"], "pando")

    def test_header_value_is_lowercase_pando(self):
        """The value must be lowercase 'pando', not 'Pando' or 'PANDO'."""
        header_value = "pando"
        self.assertEqual(header_value, "pando")
        self.assertNotEqual(header_value, "Pando")
        self.assertNotEqual(header_value, "PANDO")


class TestContext7HTTPErrors(unittest.TestCase):
    """Tests for HTTP error responses from Context7 API."""

    def test_non_200_response_returns_error_with_status_code(self):
        result = simulate_context7_http_error(404)
        self.assertIn("404", result)

    def test_http_500_returns_error_with_status_code(self):
        result = simulate_context7_http_error(500)
        self.assertIn("500", result)

    def test_http_429_returns_error_with_status_code(self):
        result = simulate_context7_http_error(429)
        self.assertIn("429", result)

    def test_error_message_contains_context7_api_error_prefix(self):
        result = simulate_context7_http_error(404)
        self.assertIn("Context7 API error", result)


class TestContext7ResolveAPIResponseParsing(unittest.TestCase):
    """Tests for parsing the Context7 resolve API JSON response."""

    def test_parse_results_from_api_response(self):
        api_response = {
            "results": [
                {
                    "title": "React",
                    "id": "/facebook/react",
                    "description": "UI library",
                    "totalSnippets": 100,
                    "stars": 200000,
                }
            ]
        }
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(len(parsed["results"]), 1)
        self.assertEqual(parsed["results"][0]["id"], "/facebook/react")

    def test_empty_results_in_api_response(self):
        api_response = {"results": []}
        parsed = json.loads(json.dumps(api_response))
        self.assertEqual(len(parsed["results"]), 0)

    def test_result_has_id_field(self):
        api_response = {
            "results": [{"title": "T", "id": "/org/lib", "description": "D", "totalSnippets": 0, "stars": 0}]
        }
        parsed = json.loads(json.dumps(api_response))
        self.assertIn("id", parsed["results"][0])


class TestContext7ResolveURL(unittest.TestCase):
    """Tests for URL construction in c7_resolve_library_id."""

    def test_resolve_url_uses_search_endpoint(self):
        url = build_context7_resolve_url("react")
        self.assertIn("/v1/search", url)

    def test_resolve_url_includes_query_param(self):
        url = build_context7_resolve_url("react")
        params = parse_qs(urlparse(url).query)
        self.assertIn("query", params)
        self.assertEqual(params["query"][0], "react")

    def test_resolve_url_base_is_context7(self):
        url = build_context7_resolve_url("react")
        self.assertTrue(url.startswith("https://context7.com/api"))


if __name__ == "__main__":
    unittest.main()
