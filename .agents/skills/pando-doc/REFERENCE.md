# pando-doc Reference

## Hugo CMS Quick Reference

### Core Concepts

Hugo is a static site generator that compiles Markdown content into HTML. It uses:
- **Content files** (Markdown with frontmatter) → compiled to pages
- **Templates** (Go HTML templates) → define page layout
- **Shortcodes** (special template tags) → reusable components
- **Config** (`hugo.toml`) → site configuration, menus, params

### Content Organization

Hugo maps filesystem structure to URL structure:
- `content/en/docs/features/cli.md` → `/en/docs/features/cli/`
- `content/en/blog/my-post.md` → `/en/blog/my-post/`

Section index files (`_index.md`) define list pages for sections.

### Frontmatter Fields

```yaml
---
title: "Page Title"        # Required: page title
date: 2026-07-27           # Required for blog posts
lastmod: 2026-07-27        # Optional: last modification date
weight: 1                  # Section ordering (lower = first)
tags: ["tag1", "tag2"]     # Taxonomy for blog posts
categories: ["cat1"]       # Another taxonomy
draft: true                # Exclude from build
slug: "custom-url"         # Override URL slug
---
```

### Shortcodes (hextra theme)

**Cards grid:**
```markdown
{{< cards >}}
  {{< card link="relative/path" title="Title" icon="icon-name" subtitle="Sub" >}}
{{< /cards >}}
```

**Hero section (homepage only):**
```markdown
{{< hextra/hero-headline >}}Main headline{{< /hextra/hero-headline >}}
{{< hextra/hero-subtitle >}}Subtitle text{{< /hextra/hero-subtitle >}}
{{< hextra/hero-button text="Button" link="url" >}}
{{< hextra/hero-badge >}}Badge{{< /hextra/hero-badge >}}
```

**Feature grid (homepage):**
```markdown
{{< hextra/feature-grid >}}
  {{< hextra/feature-card title="T" subtitle="S" icon="i" >}}
{{< /hextra/feature-grid >}}
```

**Other shortcodes:**
```markdown
{{< asciinema file="url.cast" >}}        # Terminal recording
{{< badge text="label" color="green" >}}  # Inline badge
{{< callout type="info" >}}Content{{< /callout >}}  # Callout box
{{< tabs >}}                              # Tabbed content
{{< tab title="Tab1" >}}Content{{< /tab >}}
{{< /tabs >}}
{{< steps >}}                             # Step-by-step guide
{{< /steps >}}
```

### Build Commands

```bash
hugo build              # Build to public/
hugo serve              # Dev server at localhost:1313
hugo serve -D           # Include draft posts
hugo new content/file.md  # Create from archetype
hugo --minify           # Build with minification
```

## Pando-Docs Site Structure

### Navigation Menu (hugo.toml)

| Menu Item | Path | Weight |
|-----------|------|--------|
| Docs | `/docs` | 1 |
| Blog | `/blog` | 2 |
| SDKs | `/sdk` | 3 |
| Search | (built-in) | 4 |
| GitHub | external link | 5 |

### Documentation Sections

| Section | Path | Description |
|---------|------|-------------|
| Getting Started | `docs/getting-started/` | Installation and first run |
| Configuration | `docs/configuration/` | Config file, providers, env vars |
| Features | `docs/features/` | All feature pages (largest section) |
| ACP | `docs/acp/` | Agent Client Protocol |
| MCP | `docs/mcp/` | Model Context Protocol |

### Blog Post History

| Post | Date | Tags |
|------|------|------|
| Pando May 2026: Major Feature Roundup | 2026-05-31 | Release, Features, Roundup |
| Pando June 2026: Delegation, Memory, TUI Revolution | 2026-06-28 | Release, Features, Roundup, Delegation, Memory |
| Introducing Pando SDKs | (date) | SDK, Announcement |
| Pando LLM Proxy | (date) | Features |
| Pando Desktop (Wails) | (date) | Features |
| Pando AGE Security | (date) | Security |
| Pando v1 Soon | (date) | Announcement |
| Pando is Beta | (date) | Announcement |

### Language Support

| Code | Language | Content Dir |
|------|----------|-------------|
| `en` | English | `content/en/` |
| `es` | Español | `content/es/` |

**Rule:** Every content change must be mirrored in both language directories.

## Git Workflow for Documentation Updates

### Finding Changes Since Last Tag

```bash
# List all tags
git tag --sort=-version:refname | head -10

# Get changes between two tags
git log --oneline --no-merges v0.9.0..v1.0.0

# Categorize by conventional commits
git log --oneline --no-merges v0.9.0..v1.0.0 | grep "^.*feat:"    # Features
git log --oneline --no-merges v0.9.0..v1.0.0 | grep "^.*fix:"     # Bug fixes
git log --oneline --no-merges v0.9.0..v1.0.0 | grep "^.*perf:"    # Performance
git log --oneline --no-merges v0.9.0..v1.0.0 | grep "^.*docs:"    # Doc changes

# Changes in a date range
git log --oneline --since="2026-06-01" --until="2026-07-01"

# Detailed diff of changes
git diff --stat v0.9.0..v1.0.0 -- internal/

# Files changed (grouped)
git diff --name-only v0.9.0..v1.0.0 | sort
```

### Analyzing Impact

```bash
# New files added
git diff --diff-filter=A --name-only v0.9.0..v1.0.0

# Deleted files
git diff --diff-filter=D --name-only v0.9.0..v1.0.0

# Modified files
git diff --diff-filter=M --name-only v0.9.0..v1.0.0

# Changes to public-facing features (tools, config, CLI)
git diff --stat v0.9.0..v1.0.0 -- internal/llm/agent/
git diff --stat v0.9.0..v1.0.0 -- internal/commands/
git diff --stat v0.9.0..v1.0.0 -- internal/config/
git diff --stat v0.9.0..v1.0.0 -- internal/skills/
```

### Commit Message Patterns

Pando uses conventional commits:
```
feat: add new feature
fix: resolve issue
feat(agent): delegation improvements
feat(tui): new theme support
docs: update documentation
chore: build and dependency updates
```

## Blog Post Templates

### Monthly Roundup Template

```markdown
---
title: "Pando {Month} {Year}: {Theme}"
date: {YYYY-MM-DD}
tags: ["Release", "Features", "Roundup"]
---

{Month} {Year} brought significant improvements to Pando. Here's a summary of what shipped.

## {Feature Category 1}

{Description of changes, with code examples or commands where applicable}

## {Feature Category 2}

{Description}

## Infrastructure

- Bullet list of infrastructure improvements

## What's Next

{Brief outlook for the next month}

---

*Pando is open source and under active development. Try it at [github.com/digiogithub/pando](https://github.com/digiogithub/pando).*
```

### Feature Announcement Template

```markdown
---
title: "Pando {Feature Name}"
date: {YYYY-MM-DD}
tags: ["Release", "Features", "{Specific Tag}"]
---

{Feature introduction and motivation}

## How It Works

{Technical explanation}

## Usage

{Code examples, commands, or screenshots}

## Configuration

{Any relevant config options}

---

*Pando is open source and under active development. Try it at [github.com/digiogithub/pando](https://github.com/digiogithub/pando).*
```

## Common Issues and Solutions

| Issue | Solution |
|-------|----------|
| Build fails with module error | Run `hugo mod vendor` to update vendored modules |
| New page doesn't appear | Check `_index.md` exists in parent section |
| Spanish page missing | Mirror English content to `content/es/` |
| Wrong page order | Adjust `weight` in frontmatter (lower = higher) |
| Shortcode renders as text | Ensure Hugo version supports the shortcode (check `_vendor/`) |
| Internal link broken | Use `relref` or relative paths, not absolute URLs |
