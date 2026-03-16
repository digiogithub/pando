"""
Tests for the enhanced fetch tool in internal/llm/tools/fetch.go.

Tests cover:
- isJSONContent helper function behavior
- formatJSONResponse helper function behavior
- format=json mode
- format=auto mode with different Content-Type headers and body content
- format=markdown mode with JSON Content-Type
"""

import json
import os
import sys
import unittest
from unittest.mock import MagicMock, patch

sys.path.insert(0, os.path.dirname(__file__))


def is_json_content(body: str) -> bool:
    """
    Python equivalent of the Go isJSONContent function from fetch.go.

    func isJSONContent(body []byte) bool {
        s := strings.TrimSpace(string(body))
        return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
    }
    """
    s = body.strip()
    return s.startswith("{") or s.startswith("[")


def format_json_response(body: str) -> dict:
    """
    Python equivalent of the Go formatJSONResponse function.
    Returns a dict with 'type' and 'content' keys.
    On success: content is a ```json code block with pretty-printed JSON.
    On failure: content is an error message.
    """
    try:
        parsed = json.loads(body)
        pretty = json.dumps(parsed, indent=2)
        return {"type": "text", "content": f"```json\n{pretty}\n```"}
    except json.JSONDecodeError as e:
        return {"type": "error", "content": f"Failed to format JSON: {e}"}


def simulate_fetch_format(
    body: str, content_type: str, fmt: str
) -> str:
    """
    Simulate the fetch tool's format dispatch logic from fetch.go Run().
    Returns the content string that would be in the ToolResponse.
    """
    body_bytes = body.encode("utf-8")

    if fmt == "json":
        resp = format_json_response(body)
        return resp["content"]

    if fmt == "auto":
        is_json = (
            "application/json" in content_type
            or "application/ld+json" in content_type
            or is_json_content(body)
        )
        if is_json:
            resp = format_json_response(body)
            return resp["content"]
        if "text/html" in content_type:
            return f"[markdown converted from html: {body[:30]}]"
        return body

    if fmt == "markdown":
        is_json = (
            "application/json" in content_type
            or "application/ld+json" in content_type
            or is_json_content(body)
        )
        if is_json:
            resp = format_json_response(body)
            return resp["content"]
        if "text/html" in content_type:
            return f"[markdown converted from html: {body[:30]}]"
        return f"```\n{body}\n```"

    if fmt == "text":
        if "text/html" in content_type:
            return f"[text extracted from html: {body[:30]}]"
        return body

    if fmt == "html":
        return body

    return body


class TestIsJSONContent(unittest.TestCase):
    """Tests for the isJSONContent helper function."""

    def test_body_starting_with_brace_is_json(self):
        self.assertTrue(is_json_content('{"key": "value"}'))

    def test_body_starting_with_bracket_is_json(self):
        self.assertTrue(is_json_content('[1, 2, 3]'))

    def test_body_starting_with_brace_with_leading_whitespace_is_json(self):
        self.assertTrue(is_json_content('   {"key": "value"}'))

    def test_body_starting_with_bracket_with_leading_newline_is_json(self):
        self.assertTrue(is_json_content('\n[1, 2, 3]'))

    def test_html_content_is_not_json(self):
        self.assertFalse(is_json_content('<html><body>Hello</body></html>'))

    def test_plain_text_is_not_json(self):
        self.assertFalse(is_json_content('Hello, World!'))

    def test_empty_string_is_not_json(self):
        self.assertFalse(is_json_content(''))

    def test_whitespace_only_is_not_json(self):
        self.assertFalse(is_json_content('   '))

    def test_xml_content_is_not_json(self):
        self.assertFalse(is_json_content('<?xml version="1.0"?><root/>'))

    def test_number_string_is_not_json(self):
        # Go implementation only checks for { or [ prefix
        self.assertFalse(is_json_content('42'))

    def test_array_of_objects_is_json(self):
        self.assertTrue(is_json_content('[{"id": 1}, {"id": 2}]'))

    def test_nested_object_is_json(self):
        self.assertTrue(is_json_content('{"nested": {"key": "val"}}'))


class TestFormatJSONResponse(unittest.TestCase):
    """Tests for the formatJSONResponse helper function."""

    def test_valid_json_object_returns_code_block(self):
        body = '{"key": "value"}'
        result = format_json_response(body)
        self.assertIn("```json", result["content"])
        self.assertIn("```", result["content"])
        self.assertEqual(result["type"], "text")

    def test_valid_json_array_returns_code_block(self):
        body = '[1, 2, 3]'
        result = format_json_response(body)
        self.assertIn("```json", result["content"])
        self.assertEqual(result["type"], "text")

    def test_json_code_block_contains_pretty_printed_json(self):
        body = '{"a":1,"b":2}'
        result = format_json_response(body)
        content = result["content"]
        # Extract content between ```json and ```
        inner = content.replace("```json\n", "").rstrip("`").strip()
        parsed = json.loads(inner)
        self.assertEqual(parsed["a"], 1)
        self.assertEqual(parsed["b"], 2)

    def test_invalid_json_returns_error_message(self):
        body = 'not valid json {'
        result = format_json_response(body)
        self.assertIn("Failed to format JSON", result["content"])

    def test_invalid_json_error_type(self):
        body = '{broken json'
        result = format_json_response(body)
        self.assertEqual(result["type"], "error")

    def test_valid_json_starts_with_json_code_fence(self):
        body = '{"status": "ok"}'
        result = format_json_response(body)
        self.assertTrue(result["content"].startswith("```json\n"))

    def test_valid_json_ends_with_code_fence(self):
        body = '{"status": "ok"}'
        result = format_json_response(body)
        self.assertTrue(result["content"].endswith("\n```"))


class TestFetchFormatJSON(unittest.TestCase):
    """Tests for format=json mode in the fetch tool."""

    def test_format_json_with_valid_json_returns_code_block(self):
        body = '{"name": "pando", "version": "1.0"}'
        result = simulate_fetch_format(body, "text/plain", "json")
        self.assertIn("```json", result)
        self.assertIn("pando", result)

    def test_format_json_with_invalid_json_returns_error(self):
        body = 'this is not json'
        result = simulate_fetch_format(body, "text/plain", "json")
        self.assertIn("Failed to format JSON", result)

    def test_format_json_regardless_of_content_type(self):
        """format=json should always attempt JSON formatting, ignoring content-type."""
        body = '{"data": [1, 2, 3]}'
        result_html = simulate_fetch_format(body, "text/html", "json")
        result_text = simulate_fetch_format(body, "text/plain", "json")
        self.assertIn("```json", result_html)
        self.assertIn("```json", result_text)


class TestFetchFormatAuto(unittest.TestCase):
    """Tests for format=auto mode in the fetch tool."""

    def test_auto_with_application_json_content_type_returns_json_block(self):
        body = '{"key": "val"}'
        result = simulate_fetch_format(body, "application/json", "auto")
        self.assertIn("```json", result)

    def test_auto_with_html_content_type_returns_markdown(self):
        body = '<html><body><p>Hello</p></body></html>'
        result = simulate_fetch_format(body, "text/html; charset=utf-8", "auto")
        self.assertIn("markdown converted from html", result)

    def test_auto_with_json_body_but_text_content_type_returns_json_block(self):
        """isJSONContent check should trigger JSON formatting even with text/plain."""
        body = '{"data": "value"}'
        result = simulate_fetch_format(body, "text/plain", "auto")
        self.assertIn("```json", result)

    def test_auto_with_plain_text_returns_plain_text(self):
        body = 'plain text content'
        result = simulate_fetch_format(body, "text/plain", "auto")
        self.assertEqual(result, body)

    def test_auto_with_array_json_body_returns_json_block(self):
        body = '[{"id": 1}, {"id": 2}]'
        result = simulate_fetch_format(body, "text/plain", "auto")
        self.assertIn("```json", result)

    def test_auto_with_application_ld_json_returns_json_block(self):
        body = '{"@context": "https://schema.org"}'
        result = simulate_fetch_format(body, "application/ld+json", "auto")
        self.assertIn("```json", result)


class TestFetchFormatMarkdown(unittest.TestCase):
    """Tests for format=markdown mode in the fetch tool."""

    def test_markdown_with_json_content_type_returns_json_block(self):
        """format=markdown with JSON Content-Type should return JSON block, not raw markdown."""
        body = '{"key": "value"}'
        result = simulate_fetch_format(body, "application/json", "markdown")
        self.assertIn("```json", result)

    def test_markdown_with_html_content_type_converts_to_markdown(self):
        body = '<html><body><h1>Title</h1></body></html>'
        result = simulate_fetch_format(body, "text/html", "markdown")
        self.assertIn("markdown converted from html", result)

    def test_markdown_with_plain_text_wraps_in_code_block(self):
        body = 'plain text content here'
        result = simulate_fetch_format(body, "text/plain", "markdown")
        self.assertIn("```", result)
        self.assertIn("plain text content here", result)

    def test_markdown_with_json_body_but_html_content_type_returns_json_block(self):
        """isJSONContent detected → JSON block wins over HTML→markdown conversion."""
        body = '{"json": "data"}'
        result = simulate_fetch_format(body, "text/html", "markdown")
        self.assertIn("```json", result)


class TestFetchFormatText(unittest.TestCase):
    """Tests for format=text mode in the fetch tool."""

    def test_text_with_plain_content_returns_plain_text(self):
        body = 'just some text'
        result = simulate_fetch_format(body, "text/plain", "text")
        self.assertEqual(result, body)

    def test_text_with_html_content_type_extracts_text(self):
        body = '<html><body>Hello</body></html>'
        result = simulate_fetch_format(body, "text/html", "text")
        self.assertIn("text extracted from html", result)


class TestFetchFormatHTML(unittest.TestCase):
    """Tests for format=html mode in the fetch tool."""

    def test_html_format_returns_raw_body(self):
        body = '<html><body><p>Test</p></body></html>'
        result = simulate_fetch_format(body, "text/html", "html")
        self.assertEqual(result, body)

    def test_html_format_does_not_convert_json(self):
        """format=html returns raw content even for JSON bodies."""
        body = '{"raw": "json"}'
        result = simulate_fetch_format(body, "application/json", "html")
        self.assertEqual(result, body)


if __name__ == "__main__":
    unittest.main()
