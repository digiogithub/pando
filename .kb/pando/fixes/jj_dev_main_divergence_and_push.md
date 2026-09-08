---
created_at: 2026-09-08T20:37:35.025984279Z
updated_at: 2026-09-08T20:37:35.025984279Z
tags:
    - fix
    - jj
    - vcs
    - workflow
---
# Fix: jj `dev`/`main` divergence, failed push, and the unmerged dependabot commits

Date: 2026-09-08

## Symptom

After setting a bookmark on the current change, `jj git push` appeared to fail:

```
Warning: No bookmarks/tags found in the default push revset: remote_bookmarks(remote=origin)..@
Nothing changed.
```

and the change showed as `lmzrttoq/0 ... (divergent)` with the bookmark rendered
as `main*`.

## Three separate causes

### 1. `main` is a protected branch; the move was non-fast-forward

The operation log showed `push bookmarks dev, main to git remote origin`, which
succeeded partially: `origin/dev` advanced, `origin/main` did not.

```
origin/main = f44c2e0a  "Merge pull request #14 from digiogithub/dev"
local  main = f107976c  (parent 9e6502df)
```

`main@origin` was reported `(ahead by 6 commits, behind by 1 commits)`; the
"behind by 1" is the merge commit f44c2e0a, absent from local main's ancestry, so
the push was a **sideways move**. jj allows it (force-with-lease), GitHub does
not — `gh api repos/digiogithub/pando/branches/main` returns `"protected": true`.
The repo's flow is PR `dev` -> `main`; never push `main` directly.

### 2. The bare `jj git push` warning was not an error

The default push revset is `remote_bookmarks(remote=origin)..@`. `dev@origin`
already pointed at `@` itself, so `@` is a member of `remote_bookmarks()` and the
`..` range excludes it — the revset was empty. The work was already on
`origin/dev`. `main` sat on the same commit but was never a candidate for the
same reason.

### 3. Divergent change `lmzrttoq`, and the real underlying problem

Two commits shared one change_id, both with parent 9e6502df:

```
lmzrttoq/0   f107976cae90  feat: compatibility with browser obscura + tools config fix
lmzrttoq/53  e3ffc7e50679  (empty) chore: empty change for sync
```

`jj abandon e3ffc7e50679` is refused — the commit is **immutable** because it is
an ancestor of `main@origin` (it was merged into main through a PR), and
`builtin_immutable_heads()` covers `trunk()`. It cannot and must not be abandoned.

Origin of the divergence: the empty sync change was pushed as `dev` and merged
into `main`; afterwards the *same change* was rewritten locally to carry the new
work. One evolution froze inside published `main`, the other kept moving on
`dev`. Any command resolving that change_id prefix then fails with
`Error: Change ID lmzrttoq is divergent` — address commits by bookmark name or
commit id instead.

The actionable part behind it: `main` carried real content that `dev` lacked.

```
$ jj log -r '::main@origin ~ ::dev'
f44c2e0ab900 (empty) Merge pull request #14 from digiogithub/dev
5b727a957c93 (empty) Merge pull request #12 from dependabot/go_modules-4d54e058aa
6fc668688ea9 (empty) Merge pull request #11 from dependabot/npm_and_yarn-78481560ff
452df74ef217 chore(deps): bump google.golang.org/grpc          <- real content
933d9d03f0f5 chore(deps-dev): bump browserslist                <- real content
e3ffc7e50679 (empty) chore: empty change for sync
```

## Resolution

```bash
jj bookmark set main -r main@origin --allow-backwards   # stop main tracking local work
jj new dev main -m "chore(vcs): merge main into dev"    # merge, no conflicts
jj bookmark set dev -r @
jj new                                                  # fresh mutable working copy
jj git push --bookmark dev
```

Result: `dev` = 0e94e90d (merge), `main` = `main@origin` = f44c2e0a, and
`::main@origin ~ ::dev` is now empty. The divergent change no longer carries a
bookmark.

## Verification

- Merge brought only `go.mod`, `go.sum`, `web-ui/package-lock.json`; no conflicts.
- `go build ./...` clean after the grpc bump.
- `go test ./internal/api` — ok.
- `go test ./internal/llm/agent` — 3 pre-existing failures **unrelated to the
  merge**: `TestCavemanActivatesTheSessionPolicyPath`,
  `TestCavemanSessionPolicyInstructions`,
  `TestApplyToolDiscoveryWithoutManagerIsUnchanged`. They read the project's own
  `.pando.toml` (which sets `[Caveman] DefaultMode = 'lite'`) instead of an
  isolated config, so "expected off by default, got lite". Same config-leakage
  class as the known `internal/config` test issue; not fixed here.

## Rules of thumb

- Never push `main` directly — it is protected; open a PR from `dev`.
- After pushing a change, run `jj new` before further work. Editing a change that
  is already published is what produced this divergence.
- A commit that is an ancestor of `main@origin` is immutable by design; the fix
  for a divergence involving it is to merge forward, never to abandon.

Related: [[webui_tools_settings_null_desktop_app_lists]], [[pando_repo_pitfalls]],
[[fix_config_tests_home_leakage]]
