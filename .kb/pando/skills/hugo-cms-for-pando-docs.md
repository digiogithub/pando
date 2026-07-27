---
created_at: 2026-07-27T19:54:16.902429441Z
updated_at: 2026-07-27T19:54:16.902429441Z
---
# Hugo CMS for Pando Documentation

## Overview

Pando's documentation site (`../pando-docs`) is built with Hugo, a static site generator written in Go, using the **hextra** theme. It serves user-facing documentation at https://docs.pando.dev/.

## Site Structure

### Configuration (`hugo.toml`)

- **Base URL**: `https://docs.pando.dev/`
- **Theme**: hextra (vendored in `_vendor/`)
- **Languages**: English (default, weight 1), Spanish (weight 2)
- **Menus**: Docs, Blog, SDKs, Search, GitHub, Theme Toggle, Language Switch
- **Search**: FlexSearch (built-in)
- **Blog**: Sorted by date descending, tags displayed

### Content Directory

```
content/
├── en/                          # English (default)
│   ├── _index.md                # Homepage (hextra-home layout)
│   ├── blog/                    # Blog posts
│   │   ├── _index.md            # Blog list page
│   │   └── *.md                 # Individual posts
│   ├── docs/                    # Documentation
│   │   ├── getting-started/     # Installation, quick start
│   │   ├── configuration/       # Config file, providers, env vars
│   │   ├── features/            # Feature pages (largest section)
│   │   ├── acp/                 # Agent Client Protocol
│   │   └── mcp/                 # Model Context Protocol
│   └── sdk/                     # SDK guides (python, typescript, java, dotnet)
└── es/                          # Spanish (mirrors en/)
    └── (same structure)
```

### Key Conventions

1. **Every content file needs YAML frontmatter** with `---` delimiters
2. **Blog posts require `date` and `tags`** fields
3. **Docs pages use `weight`** for sidebar ordering (lower = higher position)
4. **All changes must be mirrored** in both `en/` and `es/` directories
5. **Never edit `_vendor/`** — that's the vendored hextra theme

### Hextra Theme Shortcodes

The hextra theme provides special shortcodes:
- `{{< cards >}}` / `{{< card >}}` — Card grids for navigation
- `{{< hextra/hero-* >}}` — Homepage hero section components
- `{{< hextra/feature-grid >}}` / `{{< hextra/feature-card >}}` — Feature showcase
- `{{< asciinema >}}` — Terminal recording embeds
- `{{< callout >}}` — Callout boxes
- `{{< steps >}}` — Step-by-step guides

### Build Commands

```bash
hugo build          # Compile to public/
hugo serve          # Dev server at localhost:1313
hugo serve -D       # Include draft posts
```

## Content Generation Workflow

### From Git Changes to Blog Post

1. **Identify time range**: `git log --oneline v0.9.0..v1.0.0`
2. **Categorize changes**: Features, fixes, infrastructure, breaking changes
3. **Write blog post**: Follow roundup template in skill REFERENCE.md
4. **Update feature docs**: Create/update pages in `docs/features/`
5. **Mirror to Spanish**: Translate all new content
6. **Build and verify**: `hugo build` to check for errors

### Blog Post Templates

**Monthly Roundup:**
- Title: "Pando {Month} {Year}: {Theme}"
- Date: Last day of the month
- Tags: `["Release", "Features", "Roundup"]` + specific tags
- Structure: H2 sections for feature areas, H3 for individual features

**Feature Announcement:**
- Title: "Pando {Feature Name}"
- Date: Release date
- Tags: Feature-specific
- Structure: Introduction, How It Works, Usage, Configuration

### Documentation Page Templates

**New Feature Page:**
```yaml
---
title: Feature Name
weight: {N}
---
## Overview
## How It Works
## Usage
## Configuration
## Related Features
```

## Hextra Theme Reference

The hextra theme is vendored at `_vendor/github.com/imfing/hextra/`. Key files:
- `layouts/` — Base templates, partials, shortcodes
- `assets/css/` — Theme styles
- `i18n/` — Translation strings for 15+ languages
- `data/icons.yaml` — Available icon definitions

Theme documentation: https://imfing.github.io/hextra/docs/
