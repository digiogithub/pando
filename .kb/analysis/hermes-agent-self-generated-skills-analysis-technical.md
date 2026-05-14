# Hermes Agent: technical analysis of self-generated skills and repeated-task learning

## Objective
Produce a more technical analysis of how Hermes Agent learns from repeated tasks and turns that learning into skills, with explicit references to implementation symbols and architectural components.

## Executive conclusion
Hermes Agent implements a **review-mediated procedural learning system** rather than a simple event-frequency trigger.

The practical flow is:

1. the main agent executes a task,
2. counters and heuristics decide whether a reflective review should run,
3. a forked background agent reviews the conversation,
4. that fork may update memory and/or create or patch skills,
5. a curator subsystem maintains the long-term quality of agent-created skills.

This means “task repetition” is represented as **emergent repeated procedure and repeated corrective signal**, not just “same prompt observed N times”.

---

## 1. Architectural surfaces involved

### 1.1 Main execution loop
Primary implementation surface:
- `run_agent.py`
- class: `AIAgent`

Hermes’ self-improvement logic is attached to the main conversation loop, but does not execute inline as part of normal tool dispatch. Instead, the main loop accumulates enough context to justify a second-pass review.

### 1.2 Skill persistence surface
Primary implementation surfaces:
- `tools/skill_manager_tool.py`
- user skill storage under `~/.hermes/skills/`

This is the write path that makes learned procedures durable.

### 1.3 Curator / maintenance surface
Primary implementation surfaces:
- `agent/curator.py`
- `hermes_cli/curator.py`

This is the long-horizon controller that prevents learned skills from becoming an unbounded pile of duplicates.

---

## 2. Evidence that learning is triggered by reflective review

### 2.1 Background reviewer fork
Relevant symbol:
- `run_agent.py` → `AIAgent._spawn_background_review`

Observed semantics from symbol metadata and tests:
- It **spawns a background thread**.
- It creates a **full AIAgent fork**.
- It appends a **review prompt** as a new user turn to the forked conversation.
- It writes **directly to shared memory/skill stores**.
- It does **not modify** the main conversation history.
- It produces **no user-visible output**.

This is a strong architectural sign that Hermes treats learning as a **post-hoc reflection process**, not a synchronous action taken while the main agent is still solving the task.

### 2.2 Review is not purely memory-oriented
Relevant symbols:
- `run_agent.py` → `AIAgent._SKILL_REVIEW_PROMPT`
- `run_agent.py` → `AIAgent._spawn_background_review`

The background review explicitly targets both:
- memory saves,
- skill saves/updates.

So the system distinguishes between:
- **facts / preferences / user profile** → memory,
- **reusable procedures / workflows / operational recipes** → skills.

---

## 3. The policy encoded in `_SKILL_REVIEW_PROMPT`

Relevant symbol:
- `run_agent.py` → `AIAgent/_SKILL_REVIEW_PROMPT`

The clearest implementation clues come from prompt-focused tests in:
- `tests/run_agent/test_review_prompt_class_first.py`

These tests imply the following design contract.

### 3.1 Updating existing skills is the default
Tests indicate the prompt is intentionally biased toward:
- **patching active skills first**,
- then extending them with support files,
- then patching other existing skills,
- and only finally creating new skills.

This means Hermes is designed to learn repeated work by **consolidation before expansion**.

### 3.2 User corrections are treated as skill signals
Multiple tests assert that:
- user style corrections,
- verbosity adjustments,
- format complaints,
- and workflow corrections

must be interpreted as **first-class skill update signals**, not merely memory updates.

Technical implication:
Hermes is not only learning “the user prefers X”; it is also learning “the procedure for accomplishing X should now include this constraint”.

### 3.3 Currently loaded skills are the first patch target
The prompt contract prefers patching a skill that is already active in the session.

Technical interpretation:
If a task recurs in a context where a skill was already deemed relevant, Hermes assumes the strongest hypothesis is:
- the skill is close,
- but incomplete,
- so updating it is better than spawning a new skill namespace.

### 3.4 Support-file-first enrichment
Tests verify that the prompt explicitly names support-file classes such as:
- `references/`
- `templates/`
- `scripts/`

This is important because repeated tasks often need one of three things:
- stable reference material,
- copyable scaffolding,
- executable helpers.

Hermes therefore models skill evolution as more than markdown mutation; it can become a miniature package of procedural assets.

### 3.5 New skill creation requires a distinct class/name
The tests also enforce a “name veto” for creation.

Technical implication:
A new skill should only be created when the learned behavior is:
- distinct enough from existing skills,
- coherent as a reusable class of task,
- and nameable in a stable way.

This reduces accidental proliferation of hyper-specific skills.

### 3.6 Overlap is deferred to curator
The review prompt appears to instruct the review agent not to do live consolidation of overlapping skills, but instead **flag overlap** for curator handling.

This separates responsibilities cleanly:
- background review handles immediate procedural learning,
- curator handles structural hygiene over the whole skill library.

---

## 4. Materialization path for learned skills

Relevant symbol:
- `tools/skill_manager_tool.py` → `_create_skill`

This symbol shows how a new skill becomes durable.

### 4.1 Skill creation behavior
Observed mechanics:
- validate name,
- validate category,
- validate frontmatter,
- validate content size,
- detect collisions,
- create directory,
- atomically write `SKILL.md`,
- run security scan,
- roll back on blocked scan,
- return path + hint for adding support files.

Technical significance:
Hermes’ learned skill system is not a casual blob store. It enforces:
- naming integrity,
- content validation,
- collision avoidance,
- security review,
- atomic persistence.

This gives learned skills the same basic operational treatment as curated user-facing artifacts.

### 4.2 Support file hint confirms multi-artifact design
`_create_skill` returns a hint encouraging follow-up writes such as:
- `references/example.md`
- other companion assets.

That aligns with the review prompt’s preference for support-file enrichment and suggests the intended evolution path is:
- first create procedural core,
- then attach concrete reusable artifacts as repetition reveals need.

---

## 5. User-facing documentation confirms the intended behavior

### 5.1 README-level product promise
Relevant docs:
- `README.md`

The README describes Hermes as:
- a self-improving agent,
- with a built-in learning loop,
- that creates skills from experience,
- and improves them during use.

This is not just marketing copy; it aligns directly with the implementation surfaces above.

### 5.2 User guidance on when to create skills
Relevant docs:
- `website/docs/guides/tips.md`
- `website/docs/guides/work-with-skills.md`

The documentation states, effectively:
- if a task takes 5+ steps and will happen again, it should become a skill,
- the agent can create and update skills itself,
- these skills capture workflows and pitfalls discovered during prior work.

This is exactly the conceptual notion of “repeated task” that the implementation seems to operationalize.

### 5.3 Distinction between memory and skills
The docs explicitly frame:
- memory as “what”,
- skills as “how”.

That distinction maps directly onto the background review split.

---

## 6. Curator as long-term control system

Relevant symbols:
- `agent/curator.py` → `CURATOR_REVIEW_PROMPT`
- `agent/curator.py` → `_render_report_markdown`
- `agent/curator.py` → `_render_candidate_list`
- `agent/curator.py` → `run_curator_review`
- `hermes_cli/curator.py`

### 6.1 Curator purpose
`run_curator_review` documents a four-step flow:
1. automatic state transitions,
2. LLM review over agent-created skills,
3. state/report updates,
4. summary emission.

The CLI additionally exposes:
- pin / unpin,
- archive / restore,
- prune,
- backup / rollback,
- dry-run review.

This means the Hermes learning story is not only “create skills”, but also:
- monitor skill usage,
- identify stale or overlapping artifacts,
- archive rather than delete,
- preserve reversibility.

### 6.2 Agent-created skills as a managed category
The curator code and CLI talk specifically about **agent-created skills**.

Technical implication:
Hermes tracks provenance and gives the system permission to maintain learned skills more aggressively than bundled or manually curated ones.

### 6.3 Why this matters for repeated-task learning
In any repeated-task learner, one core risk is library bloat.

Hermes’ answer is curator-mediated lifecycle management:
- repeated tasks can become skills,
- but repeated skill creation without consolidation does not remain unchecked.

That is a meaningful engineering advantage over simpler “save every workflow as a snippet” systems.

---

## 7. Signals that count as repetition
There is no obvious first-class “repeat_count >= N ⇒ create skill” symbol in the indexed search results reviewed here. Instead, repetition appears to be inferred from a combination of contextual cues.

### 7.1 Repeated multi-step work
A procedure that repeatedly requires:
- several tools,
- ordered execution,
- non-obvious flags,
- environment-specific steps,
- or error-specific recovery

is a natural candidate for procedural persistence.

### 7.2 Repeated user correction
Tests around `_SKILL_REVIEW_PROMPT` strongly imply that repeated user complaints/corrections are elevated into skill updates.

This is particularly important because some “repeated tasks” are not repeated user intents, but repeated **agent mistakes**:
- too much verbosity,
- wrong output shape,
- wrong patch style,
- missing verification step,
- bad workflow ordering.

Hermes treats these as signals to rewrite the procedure itself.

### 7.3 Repeated skill invocation with friction
Because loaded skills are the preferred patch target, repeated use of a skill combined with repeated fixes implies a tight feedback loop:
- invoke skill,
- discover missing edge case,
- patch skill,
- next invocation improves.

That is effectively online reinforcement over procedural artifacts.

---

## 8. Comparison with a simple frequency-counter design
A naive repeated-task learner might do:
- hash prompts,
- count repeats,
- create a skill when a threshold is crossed.

Hermes does something more sophisticated.

### 8.1 Advantages of Hermes’ review-mediated design
- **Semantic generalization**: similar workflows can collapse into one skill even when prompt text differs.
- **Procedure evolution**: corrections improve existing skills instead of spawning near-duplicates.
- **Support artifact generation**: the system can choose references/templates/scripts, not just prose.
- **Human override and maintenance**: curator pin/archive/rollback controls reduce damage from over-learning.

### 8.2 Trade-offs
- **Heuristic rather than deterministic**: an LLM review can miss opportunities or overfit them.
- **Prompt-sensitive behavior**: policy quality depends on review prompt quality.
- **More complex operational surface**: background review + curator + skill manager create more moving parts than a counter-based rule.

---

## 9. Operational interpretation of “self-generated skills” in Hermes
Technically, a self-generated skill in Hermes is best described as:

- a user-home procedural artifact,
- created or patched through `skill_manage`/skill manager pathways,
- triggered by a background reflective fork of the main agent,
- validated and atomically persisted,
- then later monitored and curated as part of a managed learned-skill inventory.

That is a stronger and more maintainable architecture than plain “save a note to memory” behavior.

---

## 10. Final assessment
Hermes-agent absolutely contains a concrete mechanism for generating its own skills from repeated or reusable work patterns.

But the technically accurate phrasing is:

> Hermes does **review-driven procedural consolidation**, not just repeat-count-based skill generation.

Its design center is:
- detect repeated useful workflows and repeated corrective feedback,
- prefer patching and enriching existing skills,
- create new skills only when the behavior is distinct and reusable,
- maintain the learned library with a curator subsystem.

So if the question is “does Hermes have a mechanism where repeated tasks cause the agent to generate its own skills?” the answer is **yes**.

If the question is “is there a dedicated, simple repetition counter that directly triggers skill creation?” the answer is **not primarily**; the dominant mechanism is **background LLM review plus curator governance**.

## Referenced implementation surfaces

### Core symbols
- `run_agent.py` → `AIAgent._SKILL_REVIEW_PROMPT`
- `run_agent.py` → `AIAgent._spawn_background_review`
- `tools/skill_manager_tool.py` → `_create_skill`
- `agent/curator.py` → `CURATOR_REVIEW_PROMPT`
- `agent/curator.py` → `run_curator_review`

### Tests that define behavior
- `tests/run_agent/test_review_prompt_class_first.py`
- `tests/run_agent/test_background_review.py`
- `tests/run_agent/test_background_review_cache_parity.py`
- `tests/run_agent/test_background_review_toolset_restriction.py`

### Product and user docs
- `README.md`
- `website/docs/guides/tips.md`
- `website/docs/guides/work-with-skills.md`
- `website/docs/reference/cli-commands.md`
- release notes mentioning agent-created skills and curator behavior.