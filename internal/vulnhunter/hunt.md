You are running VULNHUNT — an adversarial security audit of the current working
project. Your job is to find *exploitable* vulnerabilities by tracing
attacker-controllable input to dangerous sinks and proving exploitability, not to
flag suspicious-looking patterns. Zero confirmed findings is a valid outcome — do
NOT soften criteria to manufacture results.

CORE PRINCIPLES (apply throughout):
- FOLLOW THE DATA. Every finding must trace a concrete path: attacker input →
  intermediate flow → dangerous sink. No data-flow, no finding.
- PROVE EXPLOITABILITY. Prefer an executable proof-of-concept or a precise,
  concrete exploit scenario. A finding without a credible exploit path is
  downgraded to "Potential", not reported as Confirmed.
- PRODUCTION CODE ONLY. Ignore tests, fixtures, build configs, vendored/generated
  code and examples unless they are actually reachable in production.
- EXHAUST ALL PATHS. Sanitization on one path does not clear an input — every
  reachable path from that input must be safe before you dismiss it.
- NEVER STOP AT ABSTRACTION BOUNDARIES. Trace through dispatchers, callbacks,
  interfaces and outbound calls to their real destinations. Audit every parameter
  of an outbound/sink call, not just the obvious one.

CONTEXT FIRST:
0. Recover prior context: search the knowledge base with kb_search_documents (no
   tags filter) for earlier security audits, known findings, threat notes, or
   architecture docs for this project. Reuse rather than rediscover.

PHASE 1 — RECON (map the attack surface):
1. Build a map of the codebase and enumerate ENTRY POINTS where external or
   user-controllable input enters: HTTP/RPC handlers and routes, CLI/argument
   parsing, file/upload parsing, deserialization, network message handlers,
   message-queue consumers, webhooks, template rendering, and any reflection or
   dynamic dispatch. Use code_get_symbols_overview, code_hybrid_search,
   code_search_pattern and grep to locate them. Record each entry point with its
   input(s) and the file:line where the input first appears.

PHASE 2 — PARALLEL HUNT (trace inputs to sinks):
2. Group the work by vulnerability CLASS and hunt classes IN PARALLEL: dispatch a
   separate hunt subagent per class with mesnada_spawn_agent (engine "claude",
   model "sonnet"), each given the recon map and a single class to trace. Cover at
   least: injection (SQL/command/template/LDAP/NoSQL), path traversal / arbitrary
   file read-write, unsafe deserialization / code execution, SSRF, authn/authz
   bypass & access control, secrets/credential exposure, and unsafe logging /
   log injection. Each subagent traces every relevant entry-point input FORWARD
   through the code to any dangerous sink and returns candidate findings with the
   full input→sink data-flow (file:line at each hop). Wait for ALL subagent
   results before continuing — do not inspect partial output.

PHASE 2b — EXPLOITABILITY VERIFY:
3. For each candidate, confirm the exploitability gates: the input is genuinely
   attacker-controllable, the path to the sink is actually reachable in
   production, and no effective control (validation, encoding, parameterization,
   authz) neutralizes it on ALL paths. Drop candidates that fail a gate.

PHASE 3 — ADVERSARIAL DISPROVE (falsify your own findings):
4. Try hard to INVALIDATE each surviving finding. Look for flawed assumptions,
   missing preconditions, logic gaps, and security controls you overlooked. A
   finding survives only if you cannot disprove it. Where feasible, construct a
   concrete proof-of-concept (an input value and the resulting dangerous call)
   demonstrating the exploit; if you cannot, downgrade the finding to "Potential".

PHASE 4 — CAPABILITY FILTER + REPORT:
5. Keep only high-priority, actionable defects. For each surviving finding record:
   a stable finding ID, title, CWE (if known), severity, the full attacker
   input→sink data-flow with file:line at each hop, the exploit scenario / PoC,
   why controls don't stop it, and a concrete remediation direction.
6. Present the findings to the user, most severe first (or state plainly that no
   exploitable vulnerability was confirmed).
7. DOCUMENT. Persist the findings report to the knowledge base with
   kb_add_document under file_path "pando/security/vulnhunt-<short-slug>.md"
   (what was audited, scope, each finding with its data-flow and PoC, and what was
   ruled out), so /vulnhunter-fix and /vulnhunt-fix-verify can recover it later.

This audit is READ-FIRST: do not modify source code. Remediation is the separate
/vulnhunter-fix workflow.
