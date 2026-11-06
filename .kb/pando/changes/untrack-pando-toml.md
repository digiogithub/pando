---
created_at: 2026-11-09T11:28:17.073844091Z
updated_at: 2026-11-09T11:28:17.073844091Z
tags:
    - change
    - chore
---
# Change: stop tracking .pando.toml

**Date:** 2026-11-09. José decided this (relayed by Pips in #115) and confirmed the push and PR directly in the session.

## What
- Commit aeb29cd4 `chore: stop tracking .pando.toml`, on top of main d6d104ea:
  - `.gitignore` gains `.pando.toml` with a comment.
  - `.pando.toml` is removed from version control with `jj file untrack`. The local file is kept.
- Bookmark `chore-untrack-pando-toml` was pushed. PR: https://github.com/digiogithub/pando/pull/33. `main` was not moved.
- No `.pando.toml.example` was added because there is no existing template without private data.

## Why
The local config can contain private hosts (an internal ollama endpoint), and the repository is public.

## How it was done (jj)
1. Appended `.pando.toml` to `.gitignore` and ran `jj file untrack .pando.toml` in the working-copy change.
2. Ran `jj split -m ... .gitignore .pando.toml`, so the commit contains only those two paths. José's uncommitted `.kb` notes stay in the child working-copy change.

## Verification
- The sha256 of the local `.pando.toml` is the same before and after the untrack and the split.
- A pattern check (values never printed) found no private-range IPs in any of the 102 committed versions of `.pando.toml` in main's history.
- Caveat: running `git pull` on a checkout that has the file deletes it from that working tree. Back it up first.
