"""Live REST checks for Model auto mode against a RUNNING Pando (PANDO-SP-0004).

Opt-in: set PANDO_E2E_BASE_URL (e.g. http://127.0.0.1:8765) and optionally
PANDO_E2E_TOKEN (sent as `Authorization: Bearer`). Without the base URL every
test is skipped. Runs under pytest or plainly: `python3 test_playground_live.py`.

The tests restore the original configuration when they finish. The router
round trip only needs the Pando server; the playground/router tests that call
the decision model additionally need a reachable decision provider and are
skipped if the server reports router_error.
"""
import copy
import json
import os
import sys
import urllib.error
import urllib.request

try:
    import pytest
except ImportError:  # plain-python mode
    pytest = None

BASE = os.environ.get("PANDO_E2E_BASE_URL", "").rstrip("/")
TOKEN = os.environ.get("PANDO_E2E_TOKEN", "")
API = "/api/v1"


class Skip(Exception):
    pass


def skip(msg):
    if pytest is not None:
        pytest.skip(msg)
    raise Skip(msg)


def need_server():
    if not BASE:
        skip("PANDO_E2E_BASE_URL not set")


def call(method, path, body=None, timeout=60):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + API + path, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if TOKEN:
        req.add_header("Authorization", "Bearer " + TOKEN)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.load(r)
    except urllib.error.HTTPError as e:
        raw = e.read().decode(errors="replace")
        try:
            return e.code, json.loads(raw)
        except ValueError:
            return e.code, {"raw": raw}


def put_body(cfg):
    """Turns a GET response into a PUT body (drops read-only fields)."""
    b = copy.deepcopy(cfg)
    for k in ("autoSelected", "warnings"):
        b.pop(k, None)
    for k in ("effectiveBaseURL", "apiKeySet", "apiKeyMasked"):
        b["router"].pop(k, None)
    return b


def draft(router_model, routes):
    return {
        "enabled": True,
        "defaultAuto": False,
        "router": {"provider": "ollama", "model": router_model, "keepAlive": "30m"},
        "threshold": 0.6,
        "routes": routes,
    }


def test_config_round_trip_masks_api_key():
    need_server()
    st, orig = call("GET", "/config/model-auto-mode")
    assert st == 200, orig
    assert "router" in orig and "routes" in orig
    restore = put_body(orig)
    try:
        body = put_body(orig)
        body["router"]["provider"] = "custom"
        body["router"]["baseURL"] = "https://gateway.invalid"
        body["router"]["model"] = body["router"].get("model") or "typesafe/jev-1.13"
        body["router"]["apiKey"] = "sk-test-1234567890"
        st, out = call("PUT", "/config/model-auto-mode", body)
        assert st == 200, out
        raw = json.dumps(out)
        assert "sk-test-1234567890" not in raw, "API key leaked in PUT response"
        assert out["router"]["apiKeySet"] is True
        assert out["router"]["apiKeyMasked"].endswith("7890")

        st, got = call("GET", "/config/model-auto-mode")
        assert st == 200
        assert "sk-test-1234567890" not in json.dumps(got), "API key leaked in GET"
        assert got["router"]["apiKeySet"] is True

        # Re-saving with the masked value keeps the stored key.
        keep = put_body(got)
        keep["router"]["apiKey"] = got["router"]["apiKeyMasked"]
        st, kept = call("PUT", "/config/model-auto-mode", keep)
        assert st == 200 and kept["router"]["apiKeySet"] is True
    finally:
        restore["clearApiKey"] = not orig["router"].get("apiKeySet")
        call("PUT", "/config/model-auto-mode", restore)


def test_invalid_config_is_rejected_with_field_errors():
    need_server()
    st, orig = call("GET", "/config/model-auto-mode")
    assert st == 200
    body = put_body(orig)
    body["routes"] = [{"id": "none", "description": "", "model": ""}]
    st, out = call("PUT", "/config/model-auto-mode", body)
    assert st == 400, out
    assert out.get("errors"), out
    assert all(e["field"].startswith("modelAutoMode.") for e in out["errors"])


def test_router_test_endpoint_reports_a_verdict():
    need_server()
    st, out = call("POST", "/model-auto-mode/router/test", {})
    # 200 with ok true/false and a report, or 400 when no router is configured.
    assert st in (200, 400), out
    if st == 200:
        assert "ok" in out and isinstance(out.get("problems", []), list)
        assert "report" in out


def test_router_test_unreachable_draft_is_not_ok():
    need_server()
    st, out = call("POST", "/model-auto-mode/router/test",
                   {"router": {"provider": "custom", "baseURL": "http://127.0.0.1:9", "model": "x"}})
    assert st in (200, 400), out
    assert out.get("ok") is False


def test_playground_blank_prompt_is_400():
    need_server()
    st, out = call("POST", "/model-auto-mode/playground", {"prompt": "  "})
    assert st == 400, out


def test_playground_with_draft_config():
    need_server()
    st, orig = call("GET", "/config/model-auto-mode")
    assert st == 200
    router_model = os.environ.get("PANDO_E2E_ROUTER_MODEL") or orig["router"].get("model") or "tev1:0.8b"
    coder_model = os.environ.get("PANDO_E2E_ROUTE_MODEL", "")
    if not coder_model:
        st, models = call("GET", "/models")
        usable = [m["id"] for m in (models.get("models") or []) if m.get("id") != "auto"]
        if not usable:
            skip("no concrete model available to target a draft route")
        coder_model = usable[0]
    routes = [
        {"id": "implementation", "model": coder_model,
         "description": "Write, modify, refactor or fix code across one or more files, including adding tests."},
        {"id": "planning", "model": coder_model,
         "description": "Design, architecture, trade-off analysis or planning a feature before implementing it."},
    ]
    st, out = call("POST", "/model-auto-mode/playground",
                   {"prompt": "add a struct with unit tests", "config": draft(router_model, routes)})
    assert st == 200, out
    d = out["decision"]
    if d["reason"] == "router_error":
        skip(f"decision provider unavailable: {d.get('errClass')} {d.get('error')}")
    assert d["routeId"] == "implementation", d
    assert d["matched"] is True and d["probability"] >= 0.6
    assert d["candidates"] and d["candidates"][0] == coder_model
    assert out["state"]

    # The draft must not have been persisted.
    st, after = call("GET", "/config/model-auto-mode")
    assert [r["id"] for r in after["routes"]] == [r["id"] for r in orig["routes"]]

    # Short follow-up -> no match, falls back to the coder (empty route id).
    st, out = call("POST", "/model-auto-mode/playground",
                   {"prompt": "ok continue", "config": draft(router_model, routes)})
    assert st == 200, out
    assert out["decision"]["matched"] is False


def test_models_list_exposes_auto_when_enabled():
    need_server()
    st, cfg = call("GET", "/config/model-auto-mode")
    assert st == 200
    if not cfg.get("enabled"):
        skip("model auto mode disabled on this server")
    st, out = call("GET", "/models")
    assert st == 200
    first = out["models"][0]
    assert first["id"] == "auto" and first["provider"] == "auto"


def _main():
    tests = [(n, f) for n, f in sorted(globals().items()) if n.startswith("test_") and callable(f)]
    failed = 0
    for name, fn in tests:
        try:
            fn()
            print(f"PASS {name}")
        except Skip as e:
            print(f"SKIP {name}: {e}")
        except AssertionError as e:
            failed += 1
            print(f"FAIL {name}: {e}")
        except Exception as e:  # noqa: BLE001
            failed += 1
            print(f"ERROR {name}: {type(e).__name__}: {e}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(_main())
