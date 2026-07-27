---
name: using-jj
description: Jujutsu (jj) version control reference. MUST load this skill whenever a query mentions jj, jujutsu, revsets, absorb, evolog, oplog, immutable_heads, divergent changes, or jj-specific concepts — jj differs from git in non-obvious ways and the skill contains critical constraints (e.g., always pass -m, never use split/diffedit) that prevent broken workflows. Skip only for trivial jj commands you're certain about (describe, new, commit, push).
---

# jj Workflow

For daily-command tables, git equivalents, troubleshooting, parallel-experiment patterns, immutable-heads disable/restore commands, recommended config, and the full revset cheatsheet, see `REFERENCE.md`.

## Philosophy

1. **Commits are cheap, descriptions are mandatory.** The working copy is always a commit. Never leave it as "(no description set)".
2. **Experiment freely, the oplog is your safety net.** `jj undo` and `jj op restore` make anything reversible.
3. **Conflicts are state, not emergencies.** jj records conflicts in commits as structured data; rebase still succeeds.
4. **Change IDs are your handle on work.** Commit hashes change on rewrite; change IDs don't.
5. **Bookmarks exist for GitHub, not for you.** Work with anonymous changes; add bookmarks only when pushing.
6. **Keep the stack shallow.** Squash early.
7. **Use `absorb` over manual squash routing.** Let jj distribute hunks to the right ancestor.
8. **Colocated = invisible to the team.** Teammates see standard git.

## CRITICAL: AI-specific rules

Always pass `-m` — unset opens an editor and blocks the agent:

```bash
jj new -m "msg"
jj describe -m "msg"
jj commit -m "msg"
jj squash -m "msg"
```

These open an editor and hang the agent (jj 0.39). Two guards enforce this —
`hooks/jj_interactive_guard.sh` blocks them pre-run, and `$JJ_EDITOR`
(`hooks/jj-reject-editor.sh`) fail-fasts any editor jj still opens — but get
them right the first time:

- `jj describe` / `jj commit` with no `-m` → add `-m "msg"`
- `jj squash` with no message → add `-m "msg"`, or `-u` to reuse the destination's
- `jj commit` / `jj squash` with `-i` / `--interactive` / `--tool` → opens the hunk
  picker even *with* `-m`; drop the flag, edit files, then `jj squash -m`
- `jj split`, `jj diffedit` → no non-interactive mode; restructure with `jj squash -m` / `jj new -m`
- `jj resolve` → edit the conflict markers in the files, then `jj squash -m`; or pass `--tool`
- `jj config edit` → use `jj config set <name> <value>`

Renamed: untrack a file with `jj file untrack <path>` — `jj forget` is gone.

## Core concepts

- Working copy = commit. Every file edit is tracked in `@`. No staging area, no `git add`.
- `@` = current change, `@-` = parent, `@--` = grandparent.
- Change IDs (e.g. `kpqxywon`) are stable across rewrites. Use these, not commit hashes.
- Conflicts are state, not emergencies — jj records them in commits and rebase still succeeds.
- Previous versions: `<change-id>/0` (latest), `/1` (previous). `jj restore --from xyz/1 --to xyz` reverts to a prior state.

## Workflows

### Squash (recommended)

```bash
jj describe -m "feat: what I'm building"
jj new -m "wip"
# ... make changes ...
jj squash -m "feat: done"
```

### Commit (simpler)

```bash
jj commit -m "feat: what I did"   # = describe + new
```

### Edit (mid-stack fix)

```bash
jj edit <change-id>
# ... fix ...
jj new -m "back to work"   # descendants auto-rebased
```

## Absorb

From `@`, `jj absorb` routes each hunk to the ancestor where those lines were last modified. Use instead of manual squash routing when fixing across a stack.

## Bookmarks & pushing

Bookmarks don't auto-advance — move them explicitly. `@` is typically empty; target `@-`.

```bash
jj bookmark set <name> -r @-
jj git push
```

## Don't rewrite reviewed PR history

If a PR has review comments, do NOT squash or rewrite the original commits — review threads detach from line anchors. Add new commits on top instead, and only rewrite after Teej confirms. See `rules/version-control.md`.

Default (safe — preserves comment anchors):

```bash
jj new feature-x- -m "address feedback"
# ... make changes ...
jj describe -m "fixup: address feedback"
jj bookmark set feature-x -r @-
jj git push
```

## Creating PRs (jj + gh)

In jj-colocated repos, git HEAD is detached — `gh pr create` fails with "not on any branch". **Always pass `--head <bookmark>`** — never rely on git auto-detection:

```bash
jj bookmark set feature-x -r @-
jj git push
gh pr create --head feature-x --title "..." --body "..."
```

## Recovery

```bash
jj op log
jj undo
jj op restore <id>
jj evolog [-r <change-id>]
```

## Immutable commits

Pushed commits are protected by `immutable_heads()`. **Always ask Teej before disabling protection** — rewriting remote bookmarks means force-pushing shared history. See `REFERENCE.md` for the disable/restore commands.

## Revsets

```bash
jj log -r 'trunk()..@'              # everything between main and here
jj log -r '::@ & ~::trunk()'         # my branch only
jj log -r 'author("trevor")'         # my commits
```

Full cheatsheet in `REFERENCE.md`.
