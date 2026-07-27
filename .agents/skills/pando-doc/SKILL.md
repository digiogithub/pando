---
name: pando-doc
description: Documentation and blog maintenance for the Pando Hugo site (pando-docs). MUST load when the task involves creating, editing, or reviewing documentation pages, blog posts, Hugo content, changelogs, release notes, or tracking code changes across commits/tags for user-facing documentation.
version: "1.0.0"
author: pando
license: MIT
compatibility: ">=0.1.0"
user-invocable: true
when-to-use: documentation, docs, blog, hugo, pando-docs, changelog, release notes, site update, markdown docs, write docs, update site, publish, content, hextra, documentation site
when-not-to-use: source code changes, internal implementation, tests, CI/CD pipelines
disable-model-invocation: false
context: |
  Skill for maintaining the Pando documentation website at ../pando-docs (or configured path).
  The site is built with Hugo + hextra theme, supports English and Spanish,
  and contains docs (getting-started, configuration, features, acp, mcp),
  blog posts (release roundups, feature announcements), and SDK guides.
---

# pando-doc — Pando Documentation & Blog Skill

## Project Location

The documentation site lives at `../pando-docs` relative to the Pando source root.
Working directory is always the pando-docs site root unless told otherwise.

## Site Architecture

```
pando-docs/
├── hugo.toml              # Hugo config (hextra module, menus, params)
├── content/
│   ├── en/                # English content (default language)
│   │   ├── _index.md      # Homepage
│   │   ├── docs/          # Documentation section
│   │   │   ├── getting-started/
│   │   │   ├── configuration/
│   │   │   ├── features/
│   │   │   ├── acp/
│   │   │   └── mcp/
│   │   ├── blog/          # Blog posts (roundups, announcements)
│   │   └── sdk/           # SDK guides (python, typescript, java, dotnet)
│   └── es/                # Spanish content (mirrors en/)
├── layouts/               # Custom layout overrides
├── static/                # Static assets (images, favicons)
├── assets/css/            # Custom CSS
├── data/                  # Hugo data files
├── i18n/                  # Translation strings
└── _vendor/               # Vendored Hugo modules (hextra theme)
```

## Hugo Content Conventions

### Frontmatter (Required)

All content files use YAML frontmatter:

```yaml
---
title: "Page Title"
date: 2026-07-27           # Blog posts require date
tags: ["Tag1", "Tag2"]     # Blog posts use tags
weight: 2                  # Section ordering (lower = higher)
---
```

### Content Types

| Type | Location | Frontmatter | Notes |
|------|----------|-------------|-------|
| **Docs** | `content/{lang}/docs/` | title, weight | Weight controls sidebar order |
| **Blog** | `content/{lang}/blog/` | title, date, tags | Monthly roundups + feature posts |
| **SDK** | `content/{lang}/sdk/` | title, weight | One per language |

### Blog Post Patterns

Monthly roundup posts follow a consistent format:
- Title: "Pando Month Year: [Theme]"
- Date: Last day of the month
- Tags: `["Release", "Features", "Roundup"]` + specific feature tags
- Sections: H2 headers for each feature area
- Footer: Standard sign-off with GitHub link

Feature announcement posts:
- Title: "Pando [Feature Name]"
- Date: Date of release
- Tags: Feature-specific tags
- Focused on one major feature with examples

### Hugo Shortcodes Used (hextra)

```markdown
{{< cards >}}
  {{< card link="path" title="Title" icon="icon-name" subtitle="Description" >}}
{{< /cards >}}

{{< hextra/hero-badge >}}Badge content{{< /hextra/hero-badge >}}
{{< hextra/hero-headline >}}Headline{{< /hextra/hero-headline >}}
{{< hextra/hero-subtitle >}}Subtitle{{< /hextra/hero-subtitle >}}
{{< hextra/hero-button text="Label" link="url" >}}
{{< hextra/feature-grid >}}
  {{< hextra/feature-card title="Title" subtitle="Text" icon="icon" >}}
{{< /hextra/feature-grid >}}

{{< asciinema file="url" >}}
```

### Icon Names

Hextra uses Heroicons (v1, outline style). Common icons:
`play`, `star`, `adjustments`, `code`, `puzzle`, `terminal`, `desktop-computer`,
`device-tablet`, `chip`, `cube`, `user-circle`, `academic-cap`, `cog`,
`wrench-screwdriver`, `calendar`, `sparkles`, `shield-check`, `bolt`, `briefcase`

## Workflow: Generating Content from Changes

### Step 1: Identify the time range

```bash
# For a specific tag range:
git log --oneline v0.9.0..v1.0.0

# For a date range:
git log --oneline --since="2026-06-01" --until="2026-07-01"

# For commits since last release tag:
git describe --tags --abbrev=0    # Find latest tag
git log --oneline $(git describe --tags --abbrev=0)..HEAD
```

### Step 2: Categorize changes

Group commits by:
1. **Features** — New capabilities (new tools, new modes, new integrations)
2. **Improvements** — Enhancements to existing features
3. **Bug fixes** — Issues resolved
4. **Infrastructure** — Performance, build, tooling
5. **Breaking changes** — API/config changes requiring user action

### Step 3: Write the blog post

Create `content/{lang}/blog/pando-{month}-{year}-roundup.md`:

```yaml
---
title: "Pando {Month} {Year}: {Theme}"
date: {last-day-of-month}
tags: ["Release", "Features", "Roundup"]
---
```

### Step 4: Update feature docs

For each new or significantly changed feature:
1. Check if a doc page exists in `content/en/docs/features/`
2. If new: create `{feature-name}.md` with proper frontmatter
3. If changed: update the existing page
4. Mirror changes to `content/es/docs/features/` (Spanish)

### Step 5: Build and verify

```bash
cd ../pando-docs && hugo build
# Check for build errors
# Verify new pages appear in the rendered HTML
```

## Multilingual Workflow

All content changes must be mirrored in both `content/en/` and `content/es/`:
- English is the primary/source language
- Spanish translations follow the same structure
- Blog post filenames differ between languages (e.g., `pando-june-2026-roundup.md` vs `pando-junio-2026-resumen.md`)
- When adding a new section, create `_index.md` in both language directories

## Critical Rules

1. **NEVER** edit files in `_vendor/` — that's the vendored hextra theme
2. **ALWAYS** use YAML frontmatter with `---` delimiters
3. **ALWAYS** mirror content changes to both `en/` and `es/`
4. **ALWAYS** run `hugo build` after changes to verify
5. Blog dates must use ISO format (YYYY-MM-DD)
6. Tags in blog posts must be arrays: `["Tag1", "Tag2"]`
7. Weights in docs control sidebar order (lower = higher position)
8. Use Hugo's `relref` or relative links for internal navigation
