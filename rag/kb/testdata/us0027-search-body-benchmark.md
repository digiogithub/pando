# PANDO-US-0027 benchmark: dropping `d.content` from vector/FTS search

Produced by `search_bench_test.go` (`BenchmarkSearchVectorScan_WithBody` /
`BenchmarkSearchVectorScan_NoBody` / `BenchmarkSearchDocumentsWithOptions`),
against a 50-document, ~2 KB-body synthetic corpus (350 chunks scanned per
query). `WithBody` replicates the pre-fix `searchVector` SELECT (which
included `d.content`, the full document body, once per scanned chunk);
`NoBody` runs the exact SQL text of the fixed production query.

Reproduce with:

```
go test ./internal/rag/kb/... -run '^$' \
  -bench '^BenchmarkSearchVectorScan_|^BenchmarkSearchDocumentsWithOptions$' \
  -benchmem -benchtime=20x
```

## Results (2026-09-14, linux/amd64, Intel Core Ultra 9 285)

| Benchmark                          | ns/op   | bytes_scanned/op | rows_scanned/op | B/op      | allocs/op |
|-------------------------------------|--------:|------------------:|-----------------:|----------:|----------:|
| SearchVectorScan_WithBody (before)  | 1,052,443 |            822,820 |             350.0 | 1,064,476 |    12,732 |
| SearchVectorScan_NoBody (after)     |   869,035 |            122,540 |             350.0 |   333,617 |    11,680 |
| SearchDocumentsWithOptions (after, full hybrid path) | 1,260,589 | — | — | 698,227 | 9,355 |

## Improvement

- **Bytes scanned**: 822,820 → 122,540 (**-85.1%**), matching the story's
  estimate that the document body is roughly 85% of the row bytes.
- **Bytes allocated**: 1,064,476 → 333,617 B/op (**-68.7%**).
- **Allocations**: 12,732 → 11,680 allocs/op (**-8.3%**; the remaining
  allocations are the per-row scan targets that are unrelated to `d.content`).
- **Row count scanned is unchanged** (350.0 in both cases): this benchmark
  isolates the effect of the column list, not row selectivity (that is
  PANDO-US-0028's path-prefix filter).

Live-database extrapolation in the story (540 documents, 6,861 chunks): body
bytes dominated at ~19.9 KB/row (~136 MB total materialised, ~238 MB
allocated). The synthetic corpus here reproduces the same ~85% body-share
ratio at a much smaller scale so the benchmark runs in CI in well under a
second.
