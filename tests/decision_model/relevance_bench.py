#!/usr/bin/env python3
"""Precision/recall benchmark of Pando's context relevance filter (PANDO-US-0096).

Replays tests/decision_model/dataset.jsonl (prompt, candidate, source, useful)
against a System One endpoint (`POST {base}/v1/systemone`, Ollama >= 0.35 or a
Jev gateway) with requests shaped exactly like
internal/llm/modelrouter/relevance.go builds them:

  state      {"request": <prompt>}
  question   cN, type "choice", instructions
             "Context item retrieved for the user's request (source: <src>):\\n"
             + candidate text (<= 400 chars) + "\\n\\nIs this item useful to
             carry out the user's request?"
  criteria   useful / not_useful with the production descriptions
  decision   keep when probabilities["useful"] >= threshold

Candidates of one prompt travel together in one request (one question each),
as in production. Two alternative framings are measured for comparison:
  score  2-anchor score question (criteria is an ARRAY ordered low -> high:
         [not useful, useful]); keep when score >= threshold. The Go client
         cannot emit this shape today (it marshals criteria as an object).
  noul   yes/no style question; keep when noul >= threshold.
Optionally --single sends one question per request ("choice-single") to expose
position/batching effects.

Environment / arguments (args win):
  PANDO_BENCH_BASE_URL  default http://localhost:11434
  PANDO_BENCH_MODEL     default tev1:0.8b
  PANDO_BENCH_API_KEY   bearer key for remote gateways

Output: a Markdown summary on stdout and, with --out, the raw JSON results.
Exit codes: 0 ok / skipped (server unreachable, unless --require),
1 ship gate not met (only with --gate), 2 hard error.
"""
import argparse
import json
import os
import statistics
import sys
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))

HEAD = "Context item retrieved for the user's request (source: %s):\n"
TAIL = "\n\nIs this item useful to carry out the user's request?"
USEFUL_DESC = "Information needed to carry out the user's request"
NOT_USEFUL_DESC = "Unrelated to the request, or only similar wording"
MAX_CHARS = 400
THRESHOLDS = (0.5, 0.6, 0.7)
GATE_PRECISION, GATE_RECALL, GATE_THRESHOLD = 0.85, 0.80, 0.60


def ellipsize(text, limit=MAX_CHARS):
    text = text.strip()
    if len(text) <= limit:
        return text
    return text[:limit].strip() + "…"


def instructions(c):
    return HEAD % c["source"] + ellipsize(c["candidate"]) + TAIL


def question(framing, c):
    ins = instructions(c)
    if framing.startswith("choice"):
        return {
            "type": "choice",
            "instructions": ins,
            "criteria": {"useful": USEFUL_DESC, "not_useful": NOT_USEFUL_DESC},
        }
    if framing == "score":
        return {"type": "score", "instructions": ins, "criteria": [NOT_USEFUL_DESC, USEFUL_DESC]}
    if framing == "noul":
        return {"type": "noul", "instructions": ins}
    raise ValueError(framing)


def p_useful(framing, ans):
    if framing.startswith("choice"):
        return ans["probabilities"]["useful"]
    if framing == "score":
        return ans["score"]
    return ans["noul"]


def post(base_url, body, api_key="", timeout=120.0):
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = "Bearer " + api_key
    req = urllib.request.Request(base_url.rstrip("/") + "/v1/systemone", json.dumps(body).encode(), headers)
    t0 = time.perf_counter()
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        data = json.load(resp)
    return data, (time.perf_counter() - t0) * 1000


def reachable(base_url, api_key=""):
    req = urllib.request.Request(base_url.rstrip("/") + "/api/version")
    if api_key:
        req.add_header("Authorization", "Bearer " + api_key)
    try:
        urllib.request.urlopen(req, timeout=5)
        return True
    except urllib.error.HTTPError:
        return True
    except (urllib.error.URLError, OSError):
        return False


def load(path):
    with open(path, encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]


def percentile(vals, q):
    s = sorted(vals)
    if not s:
        return 0.0
    return s[max(0, min(len(s) - 1, int(round(q * len(s) + 0.5)) - 1))]


def run_framing(base_url, model, framing, rows, api_key, keep_alive):
    """Returns per-row p(useful) plus latency/usage stats."""
    groups = {}
    for r in rows:
        groups.setdefault(r["prompt"], []).append(r)
    probs, req_lat, tok_in, tok_out = {}, [], 0, 0
    units = []  # (prompt, [rows]) per request
    for prompt, rs in groups.items():
        if framing == "choice-single":
            units.extend((prompt, [r]) for r in rs)
        else:
            units.append((prompt, rs))
    for prompt, rs in units:
        qs = {f"c{i + 1}": question(framing, r) for i, r in enumerate(rs)}
        body = {"model": model, "keep_alive": keep_alive, "state": {"request": prompt}, "questions": qs}
        data, dt = post(base_url, body, api_key)
        req_lat.append(dt)
        usage = data.get("usage") or {}
        tok_in += usage.get("input_tokens", 0)
        tok_out += usage.get("output_tokens", 0)
        for i, r in enumerate(rs):
            probs[r["id"]] = p_useful(framing, data["answers"][f"c{i + 1}"])
    return {
        "probs": probs,
        "request_latency_ms": req_lat,
        "candidates": len(rows),
        "requests": len(units),
        "input_tokens": tok_in,
        "output_tokens": tok_out,
        "total_ms": sum(req_lat),
    }


def metrics(rows, probs, t):
    tp = fp = fn = tn = 0
    for r in rows:
        keep = probs[r["id"]] >= t
        if keep and r["useful"]:
            tp += 1
        elif keep:
            fp += 1
        elif r["useful"]:
            fn += 1
        else:
            tn += 1
    prec = tp / (tp + fp) if tp + fp else 0.0
    rec = tp / (tp + fn) if tp + fn else 0.0
    f1 = 2 * prec * rec / (prec + rec) if prec + rec else 0.0
    return {"t": t, "tp": tp, "fp": fp, "fn": fn, "tn": tn, "precision": prec, "recall": rec, "f1": f1,
            "dropped_useful_rate": fn / (tp + fn) if tp + fn else 0.0,
            "dropped_noise_rate": tn / (tn + fp) if tn + fp else 0.0}


def auc(rows, probs):
    pos = [probs[r["id"]] for r in rows if r["useful"]]
    neg = [probs[r["id"]] for r in rows if not r["useful"]]
    if not pos or not neg:
        return 0.0
    wins = sum((p > n) + 0.5 * (p == n) for p in pos for n in neg)
    return wins / (len(pos) * len(neg))


def best_threshold(rows, probs):
    best = None
    for i in range(5, 100, 5):
        m = metrics(rows, probs, i / 100)
        if best is None or m["f1"] > best["f1"]:
            best = m
    return best


def gate_threshold(rows, probs):
    """Highest threshold-free check: lowest t on a 0.05 grid meeting the gate, or None."""
    for i in range(5, 100, 5):
        m = metrics(rows, probs, i / 100)
        if m["precision"] >= GATE_PRECISION and m["recall"] >= GATE_RECALL:
            return m
    return None


def main(argv=None):
    env = os.environ.get
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--base-url", default=env("PANDO_BENCH_BASE_URL", "http://localhost:11434"))
    ap.add_argument("--model", default=env("PANDO_BENCH_MODEL", "tev1:0.8b"))
    ap.add_argument("--api-key", default=env("PANDO_BENCH_API_KEY", ""))
    ap.add_argument("--dataset", default=os.path.join(HERE, "dataset.jsonl"))
    ap.add_argument("--framings", default="choice,score,noul,choice-single")
    ap.add_argument("--keep-alive", default="30m")
    ap.add_argument("--repeat", type=int, default=1, help="runs per framing; latency pools all runs, probabilities use the last")
    ap.add_argument("--out", default="", help="write raw JSON results here")
    ap.add_argument("--require", action="store_true", help="exit 2 instead of skipping when unreachable")
    ap.add_argument("--gate", action="store_true", help="exit 1 when the ship gate is not met by the production framing")
    a = ap.parse_args(argv)

    if not reachable(a.base_url, a.api_key):
        print(f"{'error' if a.require else 'skipped'}: {a.base_url} is unreachable")
        return 2 if a.require else 0
    rows = load(a.dataset)
    n_pos = sum(r["useful"] for r in rows)
    print(f"# Relevance filter benchmark\n\nmodel `{a.model}` @ {a.base_url}; {len(rows)} candidates "
          f"({n_pos} useful / {len(rows) - n_pos} distractors) in {len({r['prompt'] for r in rows})} prompts\n")
    try:
        t0 = time.perf_counter()
        post(a.base_url, {"model": a.model, "keep_alive": a.keep_alive, "state": {"request": "warmup"},
                          "questions": {"c1": question("choice", rows[0])}}, a.api_key)
        print(f"warm-up/cold request: {1000 * (time.perf_counter() - t0):.0f} ms\n")
        results = {}
        for fr in [f for f in a.framings.split(",") if f]:
            runs = [run_framing(a.base_url, a.model, fr, rows, a.api_key, a.keep_alive) for _ in range(a.repeat)]
            res = dict(runs[-1])
            res["request_latency_ms"] = [x for r in runs for x in r["request_latency_ms"]]
            results[fr] = res
    except urllib.error.HTTPError as e:
        print(f"error: HTTP {e.code}: {e.read().decode(errors='replace')[:300]}")
        return 2
    except (urllib.error.URLError, OSError, KeyError, ValueError) as e:
        print(f"error: {type(e).__name__}: {e}")
        return 2

    out = {"model": a.model, "base_url": a.base_url, "n": len(rows), "n_useful": n_pos, "framings": {}}
    print("## Precision / recall by threshold\n")
    print("| framing | threshold | precision | recall | F1 | TP | FP | FN | TN |")
    print("|---|---|---|---|---|---|---|---|---|")
    for fr, res in results.items():
        per_t = [metrics(rows, res["probs"], t) for t in THRESHOLDS]
        for m in per_t:
            print(f"| {fr} | {m['t']:.2f} | {m['precision']:.3f} | {m['recall']:.3f} | {m['f1']:.3f} | "
                  f"{m['tp']} | {m['fp']} | {m['fn']} | {m['tn']} |")
        bt, gt = best_threshold(rows, res["probs"]), gate_threshold(rows, res["probs"])
        out["framings"][fr] = {"thresholds": per_t, "auc": auc(rows, res["probs"]), "best_f1": bt, "gate_met_at": gt}
    print("\n## Ranking quality and best threshold (0.05 grid)\n")
    print("| framing | ROC AUC | best-F1 threshold | precision | recall | F1 | lowest threshold meeting gate (P>=0.85, R>=0.80) |")
    print("|---|---|---|---|---|---|---|")
    for fr in results:
        f = out["framings"][fr]
        b, g = f["best_f1"], f["gate_met_at"]
        print(f"| {fr} | {f['auc']:.3f} | {b['t']:.2f} | {b['precision']:.3f} | {b['recall']:.3f} | {b['f1']:.3f} | "
              f"{('%.2f' % g['t']) if g else 'none'} |")
    print("\n## Latency and tokens\n")
    print("| framing | requests | candidates | p50 / request ms | p95 / request ms | p50 / candidate ms | p95 / candidate ms | input tok | output tok |")
    print("|---|---|---|---|---|---|---|---|---|")
    for fr, res in results.items():
        lat = res["request_latency_ms"]
        per_req = res["candidates"] / res["requests"]
        out["framings"][fr].update({
            "requests": res["requests"], "candidates": res["candidates"],
            "p50_request_ms": statistics.median(lat), "p95_request_ms": percentile(lat, 0.95),
            "input_tokens": res["input_tokens"], "output_tokens": res["output_tokens"],
        })
        print(f"| {fr} | {res['requests']} | {res['candidates']} | {statistics.median(lat):.0f} | {percentile(lat, 0.95):.0f} | "
              f"{statistics.median(lat) / per_req:.0f} | {percentile(lat, 0.95) / per_req:.0f} | "
              f"{res['input_tokens']} | {res['output_tokens']} |")
    print("\nPer-candidate latency = request latency / candidates in the request (an average, not a per-question measurement).")

    by_source = {}
    if "choice" in results:
        print("\n## Production framing (choice) by source at threshold 0.60\n")
        print("| source | n | useful | precision | recall |")
        print("|---|---|---|---|---|")
        for src in ("code", "kb", "events", "memory"):
            sub = [r for r in rows if r["source"] == src]
            m = metrics(sub, results["choice"]["probs"], GATE_THRESHOLD)
            by_source[src] = m
            print(f"| {src} | {len(sub)} | {sum(r['useful'] for r in sub)} | {m['precision']:.3f} | {m['recall']:.3f} |")
        out["choice_by_source_0_60"] = by_source
        wrong = [(r, results['choice']['probs'][r['id']]) for r in rows
                 if (results['choice']['probs'][r['id']] >= GATE_THRESHOLD) != r['useful']]
        out["choice_errors_0_60"] = [{"id": r["id"], "useful": r["useful"], "p": p} for r, p in wrong]

    if a.out:
        out["probs"] = {fr: res["probs"] for fr, res in results.items()}
        with open(a.out, "w", encoding="utf-8") as f:
            json.dump(out, f, indent=1)
    if a.gate and "choice" in results:
        m = metrics(rows, results["choice"]["probs"], GATE_THRESHOLD)
        ok = m["precision"] >= GATE_PRECISION and m["recall"] >= GATE_RECALL
        print(f"\nship gate (choice, threshold {GATE_THRESHOLD}): {'MET' if ok else 'NOT MET'}")
        return 0 if ok else 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
