# jj Quick Reference

## Daily Commands

| Do this               | Command                             |
| --------------------- | ----------------------------------- |
| Start new work        | `jj new -m "what I'm building"`     |
| Describe current work | `jj describe -m "feat: what I did"` |
| Finish and move on    | `jj commit -m "feat: done"`         |
| Squash into parent    | `jj squash -m "combined message"`   |
| Auto-distribute fixes | `jj absorb`                         |
| Edit older change     | `jj edit <change-id>`               |
| Abandon a change      | `jj abandon <change-id>`            |
| View log              | `jj log`                            |
| View status           | `jj st`                             |
| View diff             | `jj diff`                           |
| Search file contents  | `jj file search "pattern"`          |

## History Surgery

```bash
jj squash -m "msg"                   # Current into parent
jj squash --from X --into Y -m "msg" # Combine any two
jj absorb                            # Auto-route fixes to right ancestors
jj rebase -r @ -d <target>           # Move current change
jj rebase -s <src> -d <dest>         # Move change + descendants
```

## File Operations

```bash
jj restore --from @- <path>          # Undo file to parent state
jj restore --from <id> <path>        # Restore from any change
jj diff <path>                       # Diff specific file
jj file show -r <id> <path>          # Show file at revision
jj file untrack <path>               # Stop tracking file (keeps on disk)
```

**Untracking files**: If jj snapshotted a file before it was added to `.gitignore`, jj keeps tracking it even after the ignore rule exists. Use `jj file untrack <path>` to remove it from jj's tree while keeping it on disk. Do NOT use `jj restore --from <parent> <path>` — that deletes the file.

## Bookmarks & Push

```bash
# Push to main
jj bookmark set master -r @-
jj git push

# New feature branch
jj bookmark create feature-x -r @-
jj git push --bookmark feature-x

# Update feature branch
jj bookmark set feature-x -r @-
jj git push

# Sync from remote
jj git fetch
jj rebase -d master@origin
```

## Recovery

```bash
jj op log                            # All operations
jj undo                              # Undo last
jj op restore <id>                   # Restore any state
jj evolog                            # Evolution of current change
jj evolog -r <change-id> -p          # Evolution with diffs
```

## Revset Cheatsheet

```text
@           Current change
@-          Parent
trunk()     Main branch
x::         Descendants of x
::x         Ancestors of x
x+          Children of x
x & y       Intersection
x | y       Union
trunk()..@  My branch
empty()     Empty commits
bookmarks() Bookmarked commits
divergent() Divergent changes
remote_tags() Remote tags
diff_lines() Commits with matching diff
xyz/0       Latest version of change xyz
xyz/1       Previous version of change xyz
```

## Git Equivalents

| Git                       | jj                                     |
| ------------------------- | -------------------------------------- |
| `git add . && git commit` | `jj commit -m "msg"`                   |
| `git commit --amend`      | Just edit files (auto-saved to `@`)    |
| `git stash`               | `jj new -m "other work" && jj edit @-` |
| `git stash pop`           | `jj edit <stashed-change-id>`          |
| `git rebase -i`           | `jj squash -m` / `jj absorb`           |
| `git reflog`              | `jj op log`                            |
| `git reset --hard`        | `jj op restore <id>`                   |
| `git branch`              | `jj bookmark`                          |
| `git log`                 | `jj log`                               |
| `git diff`                | `jj diff`                              |
| `git grep`                | `jj file search "pattern"`             |
| `git checkout <branch>`   | `jj edit <change-id>`                  |
| `git cherry-pick`         | `jj duplicate <id>`                    |

## Troubleshooting

| Problem                   | Fix                                                              |
| ------------------------- | ---------------------------------------------------------------- |
| Conflict after rebase     | Don't panic — fix files in the conflicted commit, done           |
| Lost work                 | `jj op log` then `jj op restore <id>`                            |
| Wrong parent              | `jj rebase -r @ -d <target>`                                     |
| Push rejected             | `jj git fetch && jj rebase -d master@origin`                     |
| @ is empty                | Your work is in `@-`; target `@-` for bookmarks/push             |
| Squash opens editor       | Use `jj squash -m "message"`                                     |
| "Immutable" error         | You're trying to modify a pushed commit; work on a descendant    |
| Bookmark didn't move      | Bookmarks don't auto-advance; `jj bookmark set X -r @-`          |
| New bookmark push refused | `jj config set --user 'remotes.origin.auto-track-bookmarks' '*'` |
| File tracked w/ gitignore | `jj file untrack <path>` (snapshotted before ignore rule)        |

## Parallel Experiments

```bash
jj new main -m "approach A"           # Branch from main
jj new main -m "approach B"           # Another branch from main (not from A)
jj diff --from <A-id> --to <B-id>     # Compare approaches
jj edit <winner-id>                   # Continue with the winner
jj abandon <loser-id>                 # Discard the loser
```

## Immutable Commits — Disable & Restore

When you need to rewrite a commit protected by `immutable_heads()` (e.g., squashing into a remote bookmark), **and Teej has confirmed**:

```bash
# Disable protection (quote the key — parentheses are invalid TOML bare keys)
jj config set --repo 'revset-aliases."immutable_heads()"' 'none()'

# Do your rewrite
jj squash -m "updated message"

# ALWAYS restore protection immediately after
jj config set --repo 'revset-aliases."immutable_heads()"' 'builtin_immutable_heads() | remote_bookmarks()'
```

**Note on shell quoting:** the `NAME` argument requires shell quoting around the TOML key because `immutable_heads()` contains parentheses. Use single quotes around the full dotted key with inner double quotes: `'revset-aliases."immutable_heads()"'`.

## Recommended Config

User config lives at `~/.config/jj/config.toml`:

```toml
[remotes.origin]
auto-track-bookmarks = "*"

[revset-aliases]
# Prevent rewriting pushed commits
'immutable_heads()' = 'builtin_immutable_heads() | remote_bookmarks()'
# Shorthand for trunk
'trunk()' = 'master@origin'
```

## Bail Out

```bash
rm -rf .jj    # Delete jj state, keep git unchanged
```
