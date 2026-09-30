#!/usr/bin/env python3
"""Labelled routing benchmark for Pando's Model auto mode (PANDO-SP-0004 R6).

Sends ~30 labelled developer prompts (English and Spanish) to a System One
endpoint (`POST {base}/v1/systemone`, Ollama >= 0.35 or a Jev gateway) using
the same single-choice question shape as the Pando router: 4 starter routes
plus `none`. Reports accuracy, no-match rate, p50/p95 latency and (remote
providers) cost, and exits non-zero when accuracy is below --min-accuracy.

Environment / arguments (args win):
  PANDO_BENCH_BASE_URL  base URL, default http://localhost:11434
  PANDO_BENCH_MODEL     model, default tev1:0.8b
  PANDO_BENCH_API_KEY   bearer key for remote gateways (also $TYPESAFE_API_KEY
                        when --base-url is the TypeSafe API)
  PANDO_BENCH_MIN_ACC   accuracy floor, default 0.8
  PANDO_BENCH_THRESHOLD routing threshold on p(choice), default 0.60

Exit codes: 0 ok / skipped (server unreachable, unless --require),
1 accuracy below floor, 2 unreachable with --require or hard error.
"""
import argparse
import json
import os
import statistics
import sys
import time
import urllib.error
import urllib.request

INSTRUCTIONS = (
    "A developer sent this request to an AI coding assistant. "
    "Which task category best describes it?"
)

# Route descriptions mirror the recommended starter routes of docs/model-auto-mode.md.
CRITERIA = {
    "quick_question": "Short question or explanation about code, a concept, an error message or a command; no code changes needed.",
    "implementation": "Write, modify, refactor or fix code across one or more files, including adding tests.",
    "planning": "Design, architecture, trade-off analysis or planning a feature before implementing it.",
    "docs_translation": "Write or edit documentation, README, commit messages, or translate text.",
    "none": "None of the listed tasks, or a general request.",
}

CASES = [
    # quick_question
    ("what does the -race flag do in go test?", "quick_question"),
    ("¿qué significa el error 'context deadline exceeded'?", "quick_question"),
    ("explain how the session override map works in session_overrides.go", "quick_question"),
    ("what is the difference between a mutex and an RWMutex?", "quick_question"),
    ("¿para qué sirve el comando git rebase --onto?", "quick_question"),
    ("why does my goroutine leak when the context is cancelled?", "quick_question"),
    # implementation
    ("add a ModelAutoMode struct to config.go with validation and unit tests", "implementation"),
    ("fix the nil pointer panic in prepareProvider when the account is missing", "implementation"),
    ("refactoriza el handler de modelos para separar la lógica en un servicio y añade tests", "implementation"),
    ("rename all usages of OverrideAgentModel to SetRuntimeAgentModel", "implementation"),
    ("implement retry with exponential backoff in the http client and cover it with tests", "implementation"),
    ("arregla el test que falla en internal/api por el timeout del servidor", "implementation"),
    ("add a --json flag to the doctor command that prints the report as JSON", "implementation"),
    # planning
    ("design how provider failover should work across Anthropic, Copilot and Ollama, compare options", "planning"),
    ("plan the phases to migrate the WebUI settings to the new design system", "planning"),
    ("¿cómo deberíamos arquitecturar el soporte multi-tenant? dame alternativas y trade-offs", "planning"),
    ("what are the trade-offs between SQLite WAL and a separate queue process for the event log? propose an architecture", "planning"),
    ("outline a rollout plan for splitting the monolith into services, with risks", "planning"),
    # docs_translation
    ("write a README section documenting the auto model mode", "docs_translation"),
    ("translate this paragraph to Spanish: The router picks a model per prompt.", "docs_translation"),
    ("write the commit message for these changes", "docs_translation"),
    ("escribe la documentación de la API de configuración en español", "docs_translation"),
    ("update the CHANGELOG with the new features of this release", "docs_translation"),
    # none
    ("sí, hazlo", "none"),
    ("ok continue", "none"),
    ("thanks!", "none"),
    ("vale, adelante", "none"),
    ("do it", "none"),
    ("hello", "none"),
    ("no, the other one", "none"),
]


def build_body(model, text, keep_alive="30m"):
    return {
        "model": model,
        "keep_alive": keep_alive,
        "state": {"request": text},
        "questions": {
            "task": {
                "type": "choice",
                "instructions": INSTRUCTIONS,  # real Ollama rejects an empty value
                "criteria": CRITERIA,
            }
        },
    }


def call(base_url, model, text, api_key="", timeout=120.0):
    """Returns (choice, p_choice, confidence, latency_ms, cost_or_None)."""
    url = base_url.rstrip("/") + "/v1/systemone"
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = "Bearer " + api_key
    req = urllib.request.Request(url, json.dumps(build_body(model, text)).encode(), headers)
    t0 = time.perf_counter()
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        data = json.load(resp)
    dt = (time.perf_counter() - t0) * 1000
    ans = data["answers"]["task"]
    cost = (data.get("usage") or {}).get("cost")
    return ans["choice"], ans["probabilities"][ans["choice"]], ans.get("confidence", 0.0), dt, cost


def reachable(base_url, api_key=""):
    """Cheap reachability probe: any HTTP response counts as reachable."""
    req = urllib.request.Request(base_url.rstrip("/") + "/v1/models")
    if api_key:
        req.add_header("Authorization", "Bearer " + api_key)
    try:
        urllib.request.urlopen(req, timeout=5)
        return True
    except urllib.error.HTTPError:
        return True
    except (urllib.error.URLError, OSError):
        return False


def percentile(sorted_vals, q):
    if not sorted_vals:
        return 0.0
    idx = max(0, min(len(sorted_vals) - 1, int(round(q * len(sorted_vals) + 0.5)) - 1))
    return sorted_vals[idx]


def run(base_url, model, api_key="", min_accuracy=0.8, threshold=0.60, verbose=True, label="bench"):
    """Runs the benchmark, prints the report and returns the exit code (0/1)."""
    t0 = time.perf_counter()
    call(base_url, model, "warmup", api_key)
    print(f"[{label}] {model} @ {base_url} warmup/cold {1000 * (time.perf_counter() - t0):.0f} ms")
    ok = no_match = 0
    lat, costs = [], []
    for text, expected in CASES:
        choice, p, conf, dt, cost = call(base_url, model, text, api_key)
        lat.append(dt)
        if cost is not None:
            costs.append(cost)
        # A routed decision needs p(choice) >= threshold and a real route.
        routed = choice != "none" and p >= threshold
        if not routed:
            no_match += 1
        good = choice == expected
        ok += good
        if verbose:
            print(f"{'OK ' if good else 'BAD'} exp={expected:16} got={choice:16} p={p:.2f} conf={conf:.2f} {dt:5.0f}ms | {text[:55]}")
    lat.sort()
    n = len(CASES)
    acc = ok / n
    print(
        f"[{label}] accuracy {ok}/{n} = {acc:.2f}  no-match rate {no_match / n:.2f} (threshold {threshold:.2f})  "
        f"p50 {statistics.median(lat):.0f} ms  p95 {percentile(lat, 0.95):.0f} ms"
    )
    if costs:
        print(f"[{label}] cost total ${sum(costs):.6f}  per request ${sum(costs) / len(costs):.6f}")
    if acc < min_accuracy:
        print(f"[{label}] FAIL: accuracy {acc:.2f} below floor {min_accuracy:.2f}")
        return 1
    print(f"[{label}] PASS (floor {min_accuracy:.2f})")
    return 0


def main(argv=None):
    env = os.environ.get
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=env("PANDO_BENCH_BASE_URL", "http://localhost:11434"))
    ap.add_argument("--model", default=env("PANDO_BENCH_MODEL", "tev1:0.8b"))
    ap.add_argument("--api-key", default=env("PANDO_BENCH_API_KEY", ""))
    ap.add_argument("--min-accuracy", type=float, default=float(env("PANDO_BENCH_MIN_ACC", "0.8")))
    ap.add_argument("--threshold", type=float, default=float(env("PANDO_BENCH_THRESHOLD", "0.60")))
    ap.add_argument("--require", action="store_true", help="fail (exit 2) instead of skipping when the server is unreachable")
    ap.add_argument("--quiet", action="store_true", help="only print the summary")
    a = ap.parse_args(argv)
    if not reachable(a.base_url, a.api_key):
        msg = f"skipped: {a.base_url} is unreachable"
        if a.require:
            print("error: " + msg)
            return 2
        print(msg)
        return 0
    try:
        return run(a.base_url, a.model, a.api_key, a.min_accuracy, a.threshold, verbose=not a.quiet)
    except urllib.error.HTTPError as e:
        print(f"error: HTTP {e.code}: {e.read().decode(errors='replace')[:300]}")
    except (urllib.error.URLError, OSError, KeyError, ValueError) as e:
        print(f"error: {e}")
    return 2


if __name__ == "__main__":
    sys.exit(main())
