---
created_at: 2026-07-17T11:57:25.154281538Z
updated_at: 2026-07-17T11:57:25.154281538Z
tags:
    - documentation
    - token-optimization
    - lean-ctx
    - rtk
    - pando-docs
---
# Token Optimization Documentation

## Date
2026-07-17

## What Changed
Created comprehensive user documentation for Token Optimization settings in pando-docs (English and Spanish).

## Files Created
- `/www/MCP/Pando/pando-docs/content/en/docs/configuration/token-optimization.md`
- `/www/MCP/Pando/pando-docs/content/es/docs/configuration/token-optimization.md`

## Documentation Coverage

### File Read Optimization
- **Default Read Mode**: Full, Auto, Signatures, Map options explained
- **Deduplicate Unchanged Re-reads**: Content-hash F-references explained
- **Adaptive Auto-Mode Learning**: Beta-posterior escalation layer explained

### Shell Output Optimization (RTK)
- **Enable Output Compression**: RTK-style filter pipeline for shell commands
- **Extra Filter Files**: Custom TOML filter definitions

### Code Graph
- **Build Code Property Graph**: Import/call/reference edge extraction
- **Related Files Hint**: Token-bounded list of related files

### Savings Tracking
- **Record Token-Savings Ledger**: Append-only JSONL ledger
- **Savings Widget**: Total tokens saved, percentage, breakdown by source

## Configuration Parameters (from `internal/config/config.go`)
```go
type TokenOptimizationConfig struct {
    ReadModeDefault     string  // "full" (default), "auto", "signatures", "map"
    ReadDedupDisabled   bool    // false (dedup ON)
    ReadModeLearning    bool    // false (deterministic)
    BuildCodeGraph      bool    // true
    RelatedFilesHint    bool    // false
    SavingsLedgerDisabled bool  // false (ledger ON)
}
```

## UI Components
- Web UI: `web-ui/src/components/settings/TokenOptimizationSettings.tsx`
- Settings store: `web-ui/src/stores/settingsStore.ts`

## Verification
- Hugo build successful: 107 EN pages, 106 ES pages
- No build errors

## Source Code References
- `internal/config/config.go:868-897` - TokenOptimizationConfig struct
- `internal/config/config.go:899-948` - Config resolution methods
- `web-ui/src/components/settings/TokenOptimizationSettings.tsx` - Web UI settings
- `.pando.toml:449-455` - Default configuration
