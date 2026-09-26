# Design system — default

This file is the contract every design artifact in this project is held to.
Pando reads it before designing and the reviewer reads it afterwards. Edit the
prose freely; the token table below is generated.

## Rules

- Use the tokens. Never write a raw colour, font stack, spacing value or radius
  that is not `var(--group-name)`.
- Link the generated stylesheet from every artifact entry document.
- Prefer an existing component pattern over inventing a new one.
- If a design genuinely needs a value the system does not have, add the token
  first, then use it.

## Voice

_Describe the tone this project's designs should carry._

<!-- pando:tokens:begin -->

## Tokens

Generated from `tokens.json`. Edit the tokens, not this table.

| Token | Value |
| --- | --- |
| `--color-accent` | `#2f6feb` |
| `--color-bg` | `#ffffff` |
| `--color-border` | `#d8dbe2` |
| `--color-muted` | `#5b6270` |
| `--color-surface` | `#f5f6f7` |
| `--color-text` | `#16181d` |
| `--font-mono` | `ui-monospace, SFMono-Regular, Menlo, monospace` |
| `--font-sans` | `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif` |
| `--font-scale` | `1.25` |
| `--radius-lg` | `16px` |
| `--radius-md` | `8px` |
| `--radius-sm` | `4px` |
| `--space-lg` | `32px` |
| `--space-md` | `16px` |
| `--space-sm` | `8px` |
| `--space-xl` | `64px` |
| `--space-xs` | `4px` |

<!-- pando:tokens:end -->
