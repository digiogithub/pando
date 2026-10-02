---
created_at: 2026-10-02T18:13:26.680351098Z
updated_at: 2026-10-02T18:13:26.680351098Z
---
# pando-docs rework done: features explain, guides teach, configuration is reference (2026-10-02)

Implements [[pando/plans/pando-docs-features-vs-guides-rework.md]]. Repo `../pando-docs`, uncommitted (jj working copy), not pushed.

## Result
- **Features** (46 pages x en/es): each cut to an explanation shape (what it is with an everyday simile, what it does for you, how it feels, when to use it, good to know, next steps linking to guide + reference). 33-58 lines, no config blocks. `slash-commands` stays a catalogue grouped by intent. `features/_index.md` rewritten as a plain one-line-per-feature tour by stratum.
- **Guides** (24 x en/es, none `planned`): install, first-session, setup-providers-models, choose-your-surface, webui-tour, projects-workspaces, remote-access, design-studio (surface 1-8); remembrances, mesnada, goal-mode, model-auto-mode, working-modes, self-improvement, review-and-undo (roots 10-16); mcp-servers, web-browser-desktop-tools, language-servers, editors-and-other-apps, first-skill, dev-containers, sandbox-and-permissions, save-tokens, update-and-diagnostics (soil 20-28). One `##` per Web UI step with screenshots, ending in "Check it works", "If something goes wrong", "Prefer the terminal?".
- **Reference** `docs/configuration/`: hub `_index` plus new webui, auto-mode, delegation, self-improvement, modes, tools, lsp, mcp, providers, sandbox, containers, skills-and-extensions, diagnostics, under-the-hood; rewritten remembrances, goal, token-optimization, age-encryption, security-age.
- Sidebar (`layouts/_partials/docs/sidebar.html`) groups guides by track in collapsible blocks. `docs/_index.md` explains the three page kinds and adds a Guides card.
- Voice: plain, playful, similes instead of jargon (user requirement). Spanish UI label for settings is **Configuración** (app `es.json`), not "Ajustes".
- Missing es pages created: caveman-mode, learning-mode, superpowers-mode, pando-setup-tool, mcp-authentication, lsp-auto-activation, config-discovery. en/es file parity is now complete outside the blog.
- 12 extra captures via Playwright against `pando app`: logs, self-improvement, design-artifacts, chat-slash-commands, persona-selector, model-selector, settings-snapshots, settings-api-server, settings-webui-access, orchestrator-create-task, orchestrator-new-cronjob, settings-agents-coder.

## Corrections found against binary/source while rewriting
- LLM proxy default port is 11434 (old page said 8765); collides with Ollama.
- MCP config is `[MCPServers.<name>]` with nested `.Auth`; certificate options sit at `Auth` level.
- `pando encrypt --val` does not exist; it is `pando secret`.
- Config upward search stops at `$HOME`; `PANDO_CONFIG_PARENT_SEARCH=false` disables it.
- Web UI starts with `pando app`; `pando serve` is API only.
- Projects action is "Open in new window"; once open to the network every client signs in, including the local browser.
- Caveman setting lives in Settings > General > Feedback Optimization.

## Not confirmed (described from old docs or source, not seen running)
Agent VCS revert buttons, Self-Improvement approve/reject, reasoning effort from chat model list, Copilot sign-in flow wording, proxy serving Anthropic `/v1/messages`, PWA install wording, curl example ports 3939/8766, skills catalog "Default scope" meaning, Xcode ACP setup steps (none found). `docs/acp/_index.md` "Transports" still shows `pando mcp-server --no-stdio/--no-http` which looks wrong; `docs/mcp/_index.md` still uses lowercase `[mcpServers.x]`.

## How
Coordinator wrote `scratchpad/DOCS_REWORK_BRIEF.md` and ran four parallel fork workers with disjoint file sets (A surface, B roots + modes, C soil models/protocols/hands, D soil extend/trust/operations).

## Verified
Hugo build clean with `/pando-docs/` base; link checker 27649 refs, 0 broken; no `planned` guides left; headless Chrome screenshots of a Spanish guide, the Spanish features index and the guides list. Not every page was read by the coordinator (sampled: es sandbox feature, es auto-mode guide, en webui-tour).
