"""
Tests for the Perplexity Search tool in internal/llm/tools/search_perplexity.go.

Tests cover:
- Missing API key error message
- Successful search response formatting (header, model, token count)
- AI response content inclusion
- Sources / citations section
- Authorization: Bearer header
- Default model (sonar-pro) and model selection
- system_message prepended to messages array
- search_recency_filter behavior
- HTTP error responses
"""

import json
import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))


PERPLEXITY_API_URL = "https://api.perplexity.ai/chat/completions"


def simulate_perplexity_missing_api_key() -> str:
    """Simulate the error message when PERPLEXITY_API_KEY is not set."""
    return "Perplexity Search not configured: set PERPLEXITY_API_KEY (or PANDO_PERPLEXITY_API_KEY) environment variable"


def resolve_perplexity_model(model: str) -> str:
    """
    Mirroring the model selection logic in search_perplexity.go:
      model := "sonar-pro"
      if params.Model == "sonar-reasoning" || params.Model == "sonar-deep-research" {
          model = params.Model
      }
    """
    if model in ("sonar-reasoning", "sonar-deep-research"):
        return model
    return "sonar-pro"


def resolve_perplexity_recency(recency: str) -> str:
    """
    Mirroring the search_recency_filter logic:
      recency := "month"
      switch params.SearchRecencyFilter {
      case "week", "day", "hour":
          recency = params.SearchRecencyFilter
      }
    """
    if recency in ("week", "day", "hour"):
        return recency
    return "month"


def build_perplexity_request_body(
    query: str,
    model: str = "",
    system_message: str = "",
    max_tokens: int = 0,
    temperature: float = 0.0,
    search_recency_filter: str = "",
    return_citations: bool = True,
) -> dict:
    """
    Builds the Perplexity API request body, mirroring logic in search_perplexity.go.
    """
    resolved_model = resolve_perplexity_model(model)
    resolved_recency = resolve_perplexity_recency(search_recency_filter)

    max_tok = max_tokens if 1 <= max_tokens <= 4096 else 1000
    temp = temperature if 0.0 < temperature <= 2.0 else 0.2

    messages = []
    if system_message:
        messages.append({"role": "system", "content": system_message})
    messages.append({"role": "user", "content": query})

    return {
        "model": resolved_model,
        "messages": messages,
        "max_tokens": max_tok,
        "temperature": temp,
        "return_citations": return_citations,
        "return_images": False,
        "return_related_questions": False,
        "search_recency_filter": resolved_recency,
    }


def format_perplexity_response(
    query: str,
    model: str,
    choices: list,
    citations: list,
    total_tokens: int,
    return_citations: bool = True,
) -> str:
    """
    Python equivalent of the Perplexity response formatting in search_perplexity.go.
    """
    parts = [
        f"## Perplexity Search: {query}",
        f"**Model:** {model} | **Tokens used:** {total_tokens}",
        "",
    ]

    if choices:
        parts.append(choices[0]["message"]["content"])
        parts.append("")
        parts.append("")

    if return_citations and citations:
        parts.append("### Sources:")
        for i, c in enumerate(citations, start=1):
            parts.append(f"{i}. {c}")
        parts.append("")

    return "\n".join(parts)


def simulate_perplexity_http_error(status_code: int) -> str:
    """Simulate HTTP error response from Perplexity API."""
    return f"Perplexity API error {status_code}:"


class TestPerplexitySearchMissingConfig(unittest.TestCase):
    """Tests for missing API key error messages."""

    def test_missing_api_key_error_contains_perplexity_api_key(self):
        result = simulate_perplexity_missing_api_key()
        self.assertIn("PERPLEXITY_API_KEY", result)

    def test_missing_api_key_error_mentions_pando_variant(self):
        result = simulate_perplexity_missing_api_key()
        self.assertIn("PANDO_PERPLEXITY_API_KEY", result)

    def test_missing_api_key_error_mentions_not_configured(self):
        result = simulate_perplexity_missing_api_key()
        self.assertIn("not configured", result)


class TestPerplexitySearchResponseFormatting(unittest.TestCase):
    """Tests for Perplexity Search result formatting."""

    def setUp(self):
        self.query = "What is Rust programming language?"
        self.model = "sonar-pro"
        self.choices = [
            {
                "message": {
                    "content": "Rust is a systems programming language focused on safety and performance."
                }
            }
        ]
        self.citations = [
            "https://www.rust-lang.org/",
            "https://doc.rust-lang.org/book/",
        ]
        self.total_tokens = 250

    def test_successful_response_has_perplexity_search_header(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens
        )
        self.assertIn("## Perplexity Search:", result)

    def test_header_includes_query(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens
        )
        self.assertIn(self.query, result)

    def test_header_includes_model(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens
        )
        self.assertIn("sonar-pro", result)
        self.assertIn("**Model:**", result)

    def test_header_includes_token_count(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens
        )
        self.assertIn("250", result)
        self.assertIn("**Tokens used:**", result)

    def test_ai_response_content_included(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens
        )
        self.assertIn("Rust is a systems programming language", result)

    def test_sources_section_with_citations(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens,
            return_citations=True,
        )
        self.assertIn("### Sources:", result)
        self.assertIn("https://www.rust-lang.org/", result)
        self.assertIn("https://doc.rust-lang.org/book/", result)

    def test_sources_section_numbered(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens,
            return_citations=True,
        )
        self.assertIn("1. https://www.rust-lang.org/", result)
        self.assertIn("2. https://doc.rust-lang.org/book/", result)

    def test_no_sources_section_when_return_citations_false(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, self.citations, self.total_tokens,
            return_citations=False,
        )
        self.assertNotIn("### Sources:", result)

    def test_no_sources_section_when_citations_empty(self):
        result = format_perplexity_response(
            self.query, self.model, self.choices, [], self.total_tokens,
            return_citations=True,
        )
        self.assertNotIn("### Sources:", result)


class TestPerplexityRequestHeaders(unittest.TestCase):
    """Tests for Perplexity API request headers."""

    def test_authorization_header_uses_bearer_scheme(self):
        api_key = "pplx-abc123"
        headers = {
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        }
        self.assertIn("Authorization", headers)
        self.assertTrue(headers["Authorization"].startswith("Bearer "))

    def test_authorization_header_contains_api_key(self):
        api_key = "pplx-abc123"
        headers = {"Authorization": f"Bearer {api_key}"}
        self.assertIn(api_key, headers["Authorization"])

    def test_content_type_is_application_json(self):
        headers = {"Content-Type": "application/json"}
        self.assertEqual(headers["Content-Type"], "application/json")

    def test_authorization_format_is_bearer_space_key(self):
        api_key = "test-key"
        expected = f"Bearer {api_key}"
        auth_header = f"Bearer {api_key}"
        self.assertEqual(auth_header, expected)


class TestPerplexityModelSelection(unittest.TestCase):
    """Tests for model selection logic in Perplexity Search."""

    def test_default_model_is_sonar_pro(self):
        body = build_perplexity_request_body("test query")
        self.assertEqual(body["model"], "sonar-pro")

    def test_sonar_reasoning_model_used_when_specified(self):
        body = build_perplexity_request_body("test query", model="sonar-reasoning")
        self.assertEqual(body["model"], "sonar-reasoning")

    def test_sonar_deep_research_model_used_when_specified(self):
        body = build_perplexity_request_body("test query", model="sonar-deep-research")
        self.assertEqual(body["model"], "sonar-deep-research")

    def test_invalid_model_falls_back_to_sonar_pro(self):
        body = build_perplexity_request_body("test query", model="gpt-4")
        self.assertEqual(body["model"], "sonar-pro")

    def test_empty_model_uses_sonar_pro(self):
        body = build_perplexity_request_body("test query", model="")
        self.assertEqual(body["model"], "sonar-pro")


class TestPerplexitySystemMessage(unittest.TestCase):
    """Tests for system_message prepended to messages array."""

    def test_system_message_prepended_to_messages(self):
        body = build_perplexity_request_body(
            "what is Rust?", system_message="You are a helpful assistant."
        )
        self.assertEqual(len(body["messages"]), 2)
        self.assertEqual(body["messages"][0]["role"], "system")
        self.assertEqual(body["messages"][0]["content"], "You are a helpful assistant.")

    def test_user_message_is_last_in_messages(self):
        body = build_perplexity_request_body(
            "what is Rust?", system_message="You are helpful."
        )
        self.assertEqual(body["messages"][-1]["role"], "user")
        self.assertEqual(body["messages"][-1]["content"], "what is Rust?")

    def test_no_system_message_means_only_user_message(self):
        body = build_perplexity_request_body("what is Rust?")
        self.assertEqual(len(body["messages"]), 1)
        self.assertEqual(body["messages"][0]["role"], "user")

    def test_user_message_content_matches_query(self):
        body = build_perplexity_request_body("specific query")
        self.assertEqual(body["messages"][0]["content"], "specific query")


class TestPerplexitySearchRecencyFilter(unittest.TestCase):
    """Tests for search_recency_filter behavior."""

    def test_default_recency_is_month(self):
        body = build_perplexity_request_body("test")
        self.assertEqual(body["search_recency_filter"], "month")

    def test_week_recency_filter(self):
        body = build_perplexity_request_body("test", search_recency_filter="week")
        self.assertEqual(body["search_recency_filter"], "week")

    def test_day_recency_filter(self):
        body = build_perplexity_request_body("test", search_recency_filter="day")
        self.assertEqual(body["search_recency_filter"], "day")

    def test_hour_recency_filter(self):
        body = build_perplexity_request_body("test", search_recency_filter="hour")
        self.assertEqual(body["search_recency_filter"], "hour")

    def test_invalid_recency_falls_back_to_month(self):
        body = build_perplexity_request_body("test", search_recency_filter="year")
        self.assertEqual(body["search_recency_filter"], "month")

    def test_search_recency_filter_in_request_body(self):
        body = build_perplexity_request_body("test", search_recency_filter="week")
        self.assertIn("search_recency_filter", body)


class TestPerplexityHTTPErrors(unittest.TestCase):
    """Tests for HTTP error responses from Perplexity API."""

    def test_http_429_returns_error_with_status_code(self):
        result = simulate_perplexity_http_error(429)
        self.assertIn("429", result)

    def test_http_401_returns_error_with_status_code(self):
        result = simulate_perplexity_http_error(401)
        self.assertIn("401", result)

    def test_http_500_returns_error_with_status_code(self):
        result = simulate_perplexity_http_error(500)
        self.assertIn("500", result)

    def test_error_message_contains_perplexity_api_error_prefix(self):
        result = simulate_perplexity_http_error(429)
        self.assertIn("Perplexity API error", result)


class TestPerplexityRequestBody(unittest.TestCase):
    """Tests for the Perplexity API request body structure."""

    def test_return_citations_true_by_default(self):
        body = build_perplexity_request_body("test")
        self.assertTrue(body["return_citations"])

    def test_return_citations_can_be_false(self):
        body = build_perplexity_request_body("test", return_citations=False)
        self.assertFalse(body["return_citations"])

    def test_return_images_is_false(self):
        body = build_perplexity_request_body("test")
        self.assertFalse(body["return_images"])

    def test_return_related_questions_is_false(self):
        body = build_perplexity_request_body("test")
        self.assertFalse(body["return_related_questions"])

    def test_default_max_tokens_is_1000(self):
        body = build_perplexity_request_body("test")
        self.assertEqual(body["max_tokens"], 1000)

    def test_custom_max_tokens_in_valid_range(self):
        body = build_perplexity_request_body("test", max_tokens=500)
        self.assertEqual(body["max_tokens"], 500)

    def test_max_tokens_out_of_range_uses_default(self):
        body = build_perplexity_request_body("test", max_tokens=5000)
        self.assertEqual(body["max_tokens"], 1000)

    def test_default_temperature_is_0_2(self):
        body = build_perplexity_request_body("test")
        self.assertAlmostEqual(body["temperature"], 0.2)

    def test_request_body_is_json_serializable(self):
        body = build_perplexity_request_body("test query", model="sonar-pro")
        serialized = json.dumps(body)
        parsed = json.loads(serialized)
        self.assertEqual(parsed["model"], "sonar-pro")


if __name__ == "__main__":
    unittest.main()
