# Relevance filter benchmark report

Date: 2026-10-01. Story PANDO-US-0096, epic PANDO-EP-0018.

## Setup

- Model: `tev1:0.8b` (qwen35 architecture, 752.39M parameters, Q8_0, decision capability), served by Ollama 0.35.0, endpoint `POST /v1/systemone`.
- Hardware: Intel Core Ultra 9 285 (24 threads), 93 GB RAM, NVIDIA RTX 4000 SFF Ada GPU; the model runs 100% on GPU with `num_ctx` 2050 (`ollama ps`). Other models were loaded on the same Ollama at the time.
- Script: `tests/decision_model/relevance_bench.py`; dataset: `tests/decision_model/dataset.jsonl` (72 labelled triples, 38 useful and 34 distractors, 18 prompts with 4 candidates each, sources code 38 / kb 16 / events 10 / memory 8).
- Command: `python3 tests/decision_model/relevance_bench.py --repeat 3` (latency pools the 3 runs; probabilities are identical between runs: a second run differed by 0.0 on every candidate, so the model is deterministic at this setting).
- Request shape: copied from `internal/llm/modelrouter/relevance.go` (state `{"request": prompt}`, one `choice` question per candidate with criteria `useful`/`not_useful`, candidate text capped at 400 characters, keep when p(useful) >= threshold). Candidates of one prompt travel in one request, as in production. The script talks to Ollama directly; it does not exercise Pando's Go code (the Go side is covered by `internal/llm/modelrouter/relevance_live_test.go`).
- Framings: `choice` is the production framing. `score` (criteria array `[not useful, useful]`; the Go `systemone.Question` marshals criteria as an object, so it cannot emit this shape today) and `noul` (yes/no style) are alternatives. `choice-single` is the production framing with one question per request.

## Results


| framing | threshold | precision | recall | F1 | TP | FP | FN | TN |
|---|---|---|---|---|---|---|---|---|
| choice | 0.50 | 0.744 | 0.763 | 0.753 | 29 | 10 | 9 | 24 |
| choice | 0.60 | 0.833 | 0.658 | 0.735 | 25 | 5 | 13 | 29 |
| choice | 0.70 | 0.833 | 0.395 | 0.536 | 15 | 3 | 23 | 31 |
| score | 0.50 | 0.607 | 0.895 | 0.723 | 34 | 22 | 4 | 12 |
| score | 0.60 | 0.698 | 0.789 | 0.741 | 30 | 13 | 8 | 21 |
| score | 0.70 | 0.759 | 0.579 | 0.657 | 22 | 7 | 16 | 27 |
| noul | 0.50 | 0.674 | 0.816 | 0.738 | 31 | 15 | 7 | 19 |
| noul | 0.60 | 0.778 | 0.737 | 0.757 | 28 | 8 | 10 | 26 |
| noul | 0.70 | 0.750 | 0.553 | 0.636 | 21 | 7 | 17 | 27 |
| choice-single | 0.50 | 0.535 | 1.000 | 0.697 | 38 | 33 | 0 | 1 |
| choice-single | 0.60 | 0.544 | 0.974 | 0.698 | 37 | 31 | 1 | 3 |
| choice-single | 0.70 | 0.525 | 0.842 | 0.646 | 32 | 29 | 6 | 5 |

### Ranking quality and best threshold (0.05 grid)

| framing | ROC AUC | best-F1 threshold | precision | recall | F1 | lowest threshold meeting gate (P>=0.85, R>=0.80) |
|---|---|---|---|---|---|---|
| choice | 0.786 | 0.45 | 0.750 | 0.789 | 0.769 | none |
| score | 0.774 | 0.55 | 0.667 | 0.842 | 0.744 | none |
| noul | 0.746 | 0.60 | 0.778 | 0.737 | 0.757 | none |
| choice-single | 0.536 | 0.60 | 0.544 | 0.974 | 0.698 | none |

### Latency and tokens

| framing | requests | candidates | p50 / request ms | p95 / request ms | p50 / candidate ms | p95 / candidate ms | input tok | output tok |
|---|---|---|---|---|---|---|---|---|
| choice | 18 | 72 | 163 | 239 | 41 | 60 | 40116 | 90 |
| score | 18 | 72 | 157 | 191 | 39 | 48 | 39252 | 90 |
| noul | 18 | 72 | 152 | 189 | 38 | 47 | 34356 | 90 |
| choice-single | 72 | 72 | 52 | 89 | 52 | 89 | 14745 | 72 |

Per-candidate latency = request latency / candidates in the request (an average, not a per-question measurement).

### Production framing (choice) by source at threshold 0.60

| source | n | useful | precision | recall |
|---|---|---|---|---|
| code | 38 | 22 | 0.947 | 0.818 |
| kb | 16 | 6 | 0.667 | 0.667 |
| events | 10 | 7 | 1.000 | 0.286 |
| memory | 8 | 3 | 0.333 | 0.333 |

## Ship gate

The epic gate is precision >= 0.85 AND recall >= 0.80 at the default threshold 0.60.

**Not met.** Production framing at 0.60: precision 0.833, recall 0.658, F1 0.735. No threshold on a 0.05 grid reaches both numbers for any framing (best `choice` F1 is 0.769 at 0.45 with precision 0.750 and recall 0.789). The filter therefore stays default-off (it already is) and no default or prompt was changed.

## Observations and recommendation

- Ranking quality is moderate: ROC AUC 0.79 for `choice`, 0.77 for `score`, 0.75 for `noul`. The 0.8B model separates obvious noise but is weak on semantically close distractors, which is the case the dataset deliberately stresses.
- At the default 0.60 the filter dropped 13 of 38 useful candidates (34%) while removing 29 of 34 distractors (85%). Dropping useful context is the costly error for an assistant, so the default threshold is on the wrong side of the trade-off for this model. If a user opts in, **0.50** is the better starting point (precision 0.744, recall 0.763, F1 0.753); 0.45 maximises F1 (0.769) but admits more noise. 0.70 is not advisable (recall 0.395).
- By source at 0.60 (small samples, indicative only): code is the strongest (precision 0.947, recall 0.818 on 38 candidates); KB and memory are weak (precision 0.667 and 0.333); events keep precision 1.000 but recall 0.286 (they were dropped too eagerly).
  A reasonable follow-up is to filter only code candidates by default or to use per-source thresholds; this was not implemented.
- `score` and `noul` do not beat `choice` on F1 (0.741 and 0.757 at 0.60 versus 0.735); `score` has the highest recall at 0.50 (0.895) but the lowest precision. There is no evidence to switch framing; the Go client would also need to support array criteria for `score`.
- `choice-single` (one question per request) is far worse (AUC 0.54; it keeps almost everything). Batching all candidates of a turn into one request, as production does, gave much better discrimination on this model. We did not investigate why; one hypothesis is that the other questions in the request calibrate the model, but this is untested.
- Latency (GPU, warm model): production framing p50 163 ms and p95 239 ms per request of 4 candidates, about 41 ms / 60 ms per candidate averaged. The default decision timeout (1500 ms local) leaves ample headroom; a turn with 32 candidates was not measured here.
- Tokens: the server reports about 2.2k input tokens per request of 4 candidates (40,116 input tokens and 90 output tokens for 72 candidates). The reported figure is larger than the visible text; we did not investigate how Ollama counts per-question prompt tokens.

## Caveats

- The dataset is small (72 items, 18 prompts) and was written by an LLM, then labelled by the same author; labels are judgement calls and a different annotator would disagree on some "useful" vs "close distractor" cases. Confidence intervals are wide: with 38 positives, one item moves recall by 2.6 points.
- Candidate texts imitate what the enricher formats (code symbol lines, KB excerpts, events, memories) and use real symbols, files and facts of this repository, but several line numbers, docstrings and some KB/event excerpts are illustrative and were not extracted by `code_hybrid_search`/`kb_search_documents`. Real retrieval hits may be noisier or cleaner.
- Prompts are English development requests only; no Spanish prompts, no very long candidates (400 character cap) and no turns with more than 4 candidates.
- One model (`tev1:0.8b`) on one machine. `nimble` and TypeSafe Jev were not measured.
- Only deterministic single-run probabilities are used, so there is no variance estimate beyond the dataset-size caveat.

## Reproduce

```bash
ollama pull tev1:0.8b
python3 tests/decision_model/relevance_bench.py --repeat 3 --out /tmp/relevance_bench.json
PANDO_LIVE_OLLAMA=1 go test ./internal/llm/modelrouter -run RelevanceLive -v
```

## Live Go test

`TestRelevanceLiveOllama` (`internal/llm/modelrouter/relevance_live_test.go`) on the same setup: two candidates (an obviously useful code symbol and an unrelated recipe) for a config-migration prompt; applied, 143 ms, kept the useful one (p=0.74) and dropped the unrelated one (p=0.47). The unrelated candidate scored close to the 0.60 threshold, consistent with the moderate calibration above.
