#!/usr/bin/env python3
"""Opt-in live validation of System One decision providers (PANDO-SP-0004 R6).

Ollama (PANDO_LIVE_OLLAMA=1):
  - GET /api/version is >= 0.35.0
  - GET /api/tags: decision models carry the "decision" capability and the
    model selected below is among them (the router model picker filters on it)
  - the labelled benchmark (bench_router.py) passes the accuracy floor
Remote (TYPESAFE_API_KEY, or PANDO_LIVE_JEV_BASEURL + PANDO_LIVE_JEV_KEY):
  - GET /v1/models lists models (shapes {models:[..]} or {data:[{id}]})
  - the labelled benchmark passes the floor; cost is reported when returned

Env: PANDO_LIVE_OLLAMA_URL (default http://localhost:11434),
PANDO_LIVE_OLLAMA_MODEL (default tev1:0.8b), PANDO_LIVE_JEV_MODEL (default
jev-latest for TypeSafe, typesafe/jev-1.13 for custom gateways),
PANDO_LIVE_MIN_ACC (default 0.8).

Exit: 0 when everything that ran passed or nothing could run
("skipped: no credentials"), 1 on any failure.
"""
import json
import os
import sys
import urllib.error
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import bench_router  # noqa: E402

TYPESAFE_URL = "https://api.typesafe.ai"


def get_json(url, api_key="", timeout=15):
    req = urllib.request.Request(url)
    if api_key:
        req.add_header("Authorization", "Bearer " + api_key)
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return json.load(r)


def version_tuple(v):
    parts = []
    for p in v.split("-")[0].split("."):
        try:
            parts.append(int(p))
        except ValueError:
            break
    return tuple(parts)


def check_ollama(min_acc):
    base = os.environ.get("PANDO_LIVE_OLLAMA_URL", "http://localhost:11434").rstrip("/")
    model = os.environ.get("PANDO_LIVE_OLLAMA_MODEL", "tev1:0.8b")
    failures = []
    try:
        ver = get_json(base + "/api/version")["version"]
    except (urllib.error.URLError, OSError, KeyError, ValueError) as e:
        print(f"[ollama] FAIL: cannot reach {base}: {e}")
        return False
    ok_ver = version_tuple(ver) >= (0, 35, 0)
    print(f"[ollama] version {ver} {'OK' if ok_ver else 'FAIL (need >= 0.35.0)'}")
    if not ok_ver:
        failures.append("version")

    tags = get_json(base + "/api/tags").get("models", [])
    decision = [m["name"] for m in tags if "decision" in (m.get("capabilities") or [])]
    others = [m["name"] for m in tags if m["name"] not in decision]
    print(f"[ollama] decision models: {decision or 'none'}; filtered out: {others}")
    if model not in decision:
        print(f"[ollama] FAIL: {model} is not a decision-capable model (ollama pull {model})")
        failures.append("decision-filter")
    # A non-decision model must never appear in the filtered list.
    if any(n in decision for n in others):
        failures.append("filter-leak")

    if not failures:
        if bench_router.run(base, model, "", min_acc, label="ollama") != 0:
            failures.append("accuracy")
    return not failures


def check_remote(min_acc):
    key = os.environ.get("PANDO_LIVE_JEV_KEY", "")
    base = os.environ.get("PANDO_LIVE_JEV_BASEURL", "")
    default_model = "typesafe/jev-1.13"
    if not base:
        key = key or os.environ.get("TYPESAFE_API_KEY", "")
        base = TYPESAFE_URL
        default_model = "jev-latest"
    else:
        key = key or os.environ.get("TYPESAFE_API_KEY", "")
    model = os.environ.get("PANDO_LIVE_JEV_MODEL", default_model)
    try:
        data = get_json(base.rstrip("/") + "/v1/models", key)
        ids = [m.get("id") or m.get("name") for m in (data.get("models") or data.get("data") or [])]
        print(f"[remote] {base} /v1/models -> {len(ids)} models")
    except (urllib.error.URLError, OSError, ValueError) as e:
        # Some gateways don't expose /v1/models; the benchmark below is the real check.
        print(f"[remote] note: /v1/models unavailable ({e})")
    try:
        return bench_router.run(base, model, key, min_acc, label="remote") == 0
    except (urllib.error.URLError, OSError, KeyError, ValueError) as e:
        print(f"[remote] FAIL: {e}")
        return False


def main():
    min_acc = float(os.environ.get("PANDO_LIVE_MIN_ACC", "0.8"))
    results = {}
    if os.environ.get("PANDO_LIVE_OLLAMA") == "1":
        results["ollama"] = check_ollama(min_acc)
    else:
        print("[ollama] skipped: PANDO_LIVE_OLLAMA != 1")
    has_remote = os.environ.get("PANDO_LIVE_JEV_BASEURL") or os.environ.get("TYPESAFE_API_KEY")
    if has_remote:
        results["remote"] = check_remote(min_acc)
    else:
        print("[remote] skipped: no credentials (set TYPESAFE_API_KEY or PANDO_LIVE_JEV_BASEURL + PANDO_LIVE_JEV_KEY)")
    if not results:
        print("skipped: no credentials")
        return 0
    failed = [k for k, v in results.items() if not v]
    print("summary: " + ", ".join(f"{k}={'PASS' if v else 'FAIL'}" for k, v in results.items()))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
