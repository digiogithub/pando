# Hermes Agent: analysis of self-generated skills for repeated tasks

## Summary
Hermes implements a self-improvement loop where the agent reviews completed work in the background and decides whether to persist reusable know-how as skills. The mechanism is not a simple frequency counter that says "this task happened N times, create a skill"; instead, it is a review-driven system that treats repeated friction, user corrections, complex workflows, and emerging procedures as signals to update or create skills.

## High-level model
The system has four main pieces:

1. **Skill storage and lifecycle**
   - Skills live under `~/.hermes/skills/`.
   - They can be bundled, hub-installed, plugin-provided, or agent-created.
   - Agent-created skills are first-class artifacts, but the project distinguishes them from bundled and hub-installed content.

2. **Background review trigger**
   - After conversation/tool activity thresholds, Hermes forks a background review agent.
   - That fork inspects the conversation and can write to shared memory and skill stores.
   - This happens without modifying the visible conversation.

3. **Review policy for skills**
   - The review prompt explicitly prioritizes **updating existing skills** over creating new ones.
   - The decision ladder is:
     1. patch the currently loaded skill,
     2. add support files to an existing skill,
     3. patch another existing skill,
     4. only then create a new skill if the behavior is distinct and nameable.

4. **Long-term maintenance via curator**
   - A separate curator process reviews agent-created skills over time.
   - It tracks usage, staleness, overlap, pinning, archival, and backups.
   - This is the mechanism that keeps the learned skill set from degrading into duplication and clutter.

## What “repeated task” means in practice
In Hermes, repetition is inferred semantically rather than counted mechanically.

A task tends to become skill-worthy when one or more of these patterns recur:

- the user asks for the same multi-step workflow repeatedly,
- the agent follows the same procedure across sessions,
- the user repeatedly corrects style, verbosity, formatting, or execution behavior,
- the agent discovers a reliable tool sequence with non-obvious steps or pitfalls,
- the task benefits from support artifacts like scripts, templates, or references.

This is reinforced by the project docs:

- the README describes Hermes as a self-improving agent that “creates skills from experience” and “improves them during use”,
- the user docs recommend creating a skill when a task takes 5+ steps and will be done again,
- release notes repeatedly mention agent-created skills, self-improvement, and curator maintenance.

## Concrete implementation evidence

### 1. Background review agent
`run_agent.py` contains `_spawn_background_review`, whose documented role is:

- create a full `AIAgent` fork,
- append a review prompt as the next user turn,
- write directly to shared memory/skill stores,
- never modify the main conversation.

This shows skill creation is driven by a **secondary reflective pass**, not inline during the main tool loop.

### 2. Dedicated skill review prompt
`run_agent.py` also contains `_SKILL_REVIEW_PROMPT`.
From the associated tests in `tests/run_agent/test_review_prompt_class_first.py`, the policy is very explicit:

- updating active skills is the default stance,
- user corrections are treated as a strong signal for skill evolution,
- currently loaded skills are the first patch target,
- support files should be preferred before birthing new skills,
- new skill creation requires a distinct class/name,
- overlap should be flagged for curator review rather than merged ad hoc,
- “Nothing to save” remains possible, but is not the preferred outcome.

This is the clearest evidence that Hermes learns from repeated tasks by **refactoring learned behavior into reusable procedural memory**.

### 3. Agent-created skill management APIs
`tools/skill_manager_tool.py` exposes `_create_skill`, showing that agent-authored skills are materialized as actual skill directories with `SKILL.md` and optional support files. The returned hint nudges follow-up enrichment with `references/`, `templates/`, or `scripts/`.

This matters because it means the learned result is not just a note in memory; it becomes executable procedural documentation.

### 4. Curator as second-order control system
`agent/curator.py` contains:

- `CURATOR_REVIEW_PROMPT`,
- reporting helpers,
- `run_curator_review`.

Its documented flow is:

1. apply automatic transitions,
2. if there are agent-created skills, run an LLM review over candidates,
3. update curator state and reports,
4. summarize the pass.

The CLI exposes pin, unpin, archive, restore, prune, backup, rollback. This shows Hermes treats self-generated skills as a growing knowledge base that needs curation, not just accumulation.

## Architecture interpretation
The mechanism is best understood as a **closed procedural learning loop**:

1. **Do work** with tools.
2. **Observe patterns** and failures in completed work.
3. **Reflect asynchronously** in a forked agent.
4. **Persist reusable process knowledge** as skill updates or new skills.
5. **Curate over time** to avoid duplication and staleness.

That is more advanced than static prompt memory and more conservative than automatic skill generation on every repeated task. It balances:

- learning speed,
- prompt budget,
- maintainability,
- user control.

## Strengths of Hermes’ approach

### Conservative creation policy
By preferring patches to existing skills first, Hermes reduces fragmentation. This is important because naive self-improvement systems often create too many overlapping skills.

### Support-file aware learning
The review policy explicitly allows learning to spill into:

- `references/`,
- `templates/`,
- `scripts/`.

That is a strong design choice because repeated tasks often need more than prose instructions.

### Separation of online execution and offline reflection
The main agent loop stays focused on solving the user task, while learning happens in the background. This lowers interference with immediate task completion.

### Curator safeguards
Pinning, archival, backups, rollback, and overlap handling indicate Hermes was designed with the expectation that self-generated skills can become noisy unless managed.

## Limitations / caveats

### Not purely frequency-based
If one expects a deterministic repeated-task counter, Hermes does not appear to expose one as the primary driver. The learning decision is prompt-mediated and therefore heuristic.

### Quality depends on review prompt quality
Because the review pass is LLM-driven, false positives and false negatives are possible:

- a repeated task might remain only in memory,
- a one-off task might be generalized too aggressively,
- stylistic corrections might be overfit into a skill.

### Learned skills are local to the Hermes home/profile
The lifecycle is profile- and home-aware. This is good for isolation, but means procedural learning is distributed unless explicitly exported/imported.

## Comparison to simpler agent systems
Compared with typical agents that only have:

- transient conversation context,
- a static skill library,
- optional user-authored notes,

Hermes adds:

- agent-authored skills,
- background learning reviews,
- curator maintenance,
- integration between conversation review and procedural artifacts.

This makes it closer to a practical self-improving agent than most CLI assistants.

## Conclusion
Hermes-agent does have a real mechanism for generating its own skills when tasks repeat or when reusable procedures emerge, but the trigger is **reflective and policy-based**, not a blunt repetition counter.

The core design is:

- use background review to detect reusable patterns,
- prefer updating existing skills over creating new ones,
- materialize learned procedures as actual skill directories and support files,
- maintain the resulting skill set with a curator.

In short: Hermes learns repeated work as **procedural assets**, not just as memory facts, and it includes the operational machinery needed to keep that learned library usable over time.