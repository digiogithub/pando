---
created_at: 2026-09-01T21:10:39.626245226Z
updated_at: 2026-09-01T21:10:39.626245226Z
tags:
    - changes
    - documentation
    - hugo
    - pando-docs
    - august-2026
---
# Pando docs site — August 2026 documentation + monthly blog post

Date: 2026-09-01. Target repo: `../pando-docs` (Hugo + hextra, bilingual en/es).

## Motivation

Document every user-facing change merged into `pando` between 2026-08-01 and 2026-09-01, in the
public documentation site, plus the monthly roundup blog post. Audience is the end user: what the
feature does, how to enable and configure it — no internal implementation detail.

## Source material reviewed

`git log --since=2026-07-31 --until=2026-09-02` (36 commits) plus the KB docs committed in the
repo during August: `.kb/pando/features/design_p0..p8`, `.kb/pando/plans/pando_design_studio_plan.md`,
`.kb/pando/changes/designer_always_on_autoopen_live_reload.md`, `.kb/pando/changes/uiauto_*`,
`docs/desktop-controller.md`, `docs/extension-{mechanisms,builds,authoring,frontend,memory}.md`,
`.kb/pando/features/tool-discovery-gateway-unification.md`,
`.kb/pando/changes/issue6-thinking-reasoning-modes-impl.md`,
`.kb/pando/fixes/copilot_api_token_exchange_byok_custom_models.md`,
`.kb/pando/features/external_access_footer_toggle.md`,
`.kb/pando/features/context-enrichment-agent-loop.md`,
`.kb/pando/changes/toon-go-fork-spec-v4.1.md`, `cmd/design.go`, `cmd/agui_serve.go`,
`internal/config/config.go`.

## New pages (created in both en and es)

| Path (en / es) | Covers |
|---|---|
| `docs/features/design-studio.md` | Pando Designer v1: always-on studio, artifacts in the working tree, auto-open preview + live reload across TUI/WebUI/desktop/ACP, versions, design system (`init`/`show`/`examples`/`extract` from code/url/image/text, `apply`), bundled templates + craft references, critic quality gate (`[Design.Critique]`), `pando design` CLI, `[Design]` config, external preview sharing, `[MCPServer.Design]` exposure |
| `docs/features/desktop-controller.md` | Desktop AI automation: accessibility-tree-first rationale vs screenshot agents, capability/permission table, `DesktopEnabled` + all `[InternalTools] Desktop*` keys, browser-as-an-app routing and `desktop_*` vs `browser_*` guidance, per-platform prerequisites (X11/Wayland portal/macOS permissions), honest maturity matrix, MCP exposure, security section |
| `docs/features/extensions.md` | Extension system: five-mechanism decision table, when an extension is justified, `pando extensions list` / `pando ext` / variant stamp, `[Extensions]` + `[Extensions.Entries]` config, what an extension may contribute, enterprise builds (`make build-enterprise`, `xpando build` flags), frontend embedding caveat |
| `docs/features/reasoning-modes.md` | Per-model reasoning-effort resolution (issue #6): why values differ per model, clamping/defaulting, where to change it (TUI/WebUI/ACP/`[Agents.*].ReasoningEffort`), practical effort guidance |

## Pages updated (en + es)

- `docs/features/tool-discovery.md` — `tool_search` now searches *and* executes (internal + MCP);
  single `ToolDiscovery.Enabled` switch replacing the separate MCP gateway mechanism; favourites
  stay direct, rest of catalog behind search.
- `docs/features/copilot-auth.md` — new "Organization BYOK models" section (Business/Enterprise
  custom models now listed, nothing to configure, troubleshooting via `auth copilot status`).
- `docs/features/webui-access.md` — new "external access without a restart" section for the WebUI
  footer toggle; basic auth enforced immediately on enabling.
- `docs/features/context-enrichment.md` — new "enrichment as an agent loop" section:
  `[Agents.context-enricher]` model, the `ContextEnrichmentAgentLoop*` keys, session-start-only
  default, chat notices, child session visibility, fallback, warm start.
- `docs/features/web-ui.md` — added bullets: Design page, footer external-access toggle, lazy
  session list, working dir in chat info, KB folder browser, per-provider embeddings selector.
- `sdk/typescript.md` — AG-UI `--persona` flag and `[AGUI].Persona` config.

## Blog posts (new)

- `content/en/blog/pando-august-2026-roundup.md` — "Pando August 2026: Design Studio, Desktop
  Control, and Extensions" (date 2026-08-31).
- `content/es/blog/pando-agosto-2026-resumen.md` — Spanish counterpart, following the existing
  `/es/...` internal-link convention of the site.

Both cover: Design Studio, Desktop Controller, Extensions, unified tool discovery, per-model
reasoning effort, Copilot org BYOK models, footer external access, context-enrichment agent loop,
WebUI improvements (lazy sessions, modified files, agentVCS delta, instances panel, reconnect of
pending AskUserQuestion, external links), and shorter items (AG-UI personas, fresh-install
provider/model defaults, macOS desktop clipboard/startup fixes, TOON 4.1, security/dependency
hardening).

## Verification

- `hugo --gc --minify` builds clean, no warnings; all new pages and both blog posts render under
  `docs/features/{design-studio,desktop-controller,extensions,reasoning-modes}` and
  `blog/pando-august-2026-roundup` / `es/blog/pando-agosto-2026-resumen`.

Related: [[pando_design_studio_plan]] · [[desktop_controller_uiauto_plan]] ·
[[extension_system_enterprise_analysis]] · [[tool-discovery-gateway-unification]] ·
[[external_access_footer_toggle]] · [[context-enrichment-agent-loop]] ·
[[copilot_api_token_exchange_byok_custom_models]] · [[july-2026-docs-update]]
