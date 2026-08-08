---
created_at: 2026-08-16T19:12:05.22694603Z
updated_at: 2026-08-16T19:14:17.871456044Z
---
# GitHub CLI (`gh`) for working with issues in the `digiogithub/pando` project

This note documents how to use the GitHub CLI (`gh`) and its REST API to inspect, query, and manage issues for the Pando repository.

## 1. Authentication and repository context

First check that the CLI is installed and authenticated:

```bash
gh --version
gh auth status
```

If you need to log in explicitly:

```bash
gh auth login
```

For a specific repository, use the `--repo` flag or set the current repo context:

```bash
gh repo view digiogithub/pando
```

The current repository context for this project is:

- `digiogithub/pando`
- `main` branch
- public repository

## 2. Listing open issues

List all currently open issues in the repository:

```bash
gh issue list --repo digiogithub/pando --state open --limit 20
```

Or in a JSON-friendly form:

```bash
gh issue list --repo digiogithub/pando --state open --limit 20 --json number,title,state,url,author,createdAt
```

For compact output with less tokens, `yq` can convert the JSON stream to YAML:

```bash
gh issue list --repo digiogithub/pando --state open --limit 20 --json number,title,state,url | yq -y '.'
```

Tested result for this project:

```yaml
- number: 6
  state: OPEN
  title: thinking ot reasoning mode os different by model, default value gets errors
  url: https://github.com/digiogithub/pando/issues/6
- number: 5
  state: OPEN
  title: Error no auto compact in ACP mode
  url: https://github.com/digiogithub/pando/issues/5
```

There are currently two open issues in the project:

- #5 — `Error no auto compact in ACP mode`
- #6 — `thinking ot reasoning mode os different by model, default value gets errors`

## 3. Viewing a single issue

Open a specific issue by number:

```bash
gh issue view 5 --repo digiogithub/pando
```

Or open it in the browser:

```bash
gh issue view 5 --repo digiogithub/pando --web
```

Useful variant:

```bash
gh issue view 5 --repo digiogithub/pando --comments
```

## 4. Using the REST API with `gh api`

`gh api` is the direct gateway to GitHub's API and is very useful for scripting and quick inspection.

List all open issues via the REST endpoint:

```bash
gh api repos/digiogithub/pando/issues --paginate --jq '.[] | select(.state=="open") | {number,title,state,url}'
```

Equivalent simplified command:

```bash
gh api repos/digiogithub/pando/issues --jq '.[] | select(.state=="open") | {number,title,state,url}'
```

If you want YAML instead of JSON, pipe to `yq -y`:

```bash
gh api repos/digiogithub/pando/issues --jq '.[] | select(.state=="open") | {number,title,state,url}' | yq -y '.'
```

Example output from the project:

```yaml
- number: 6
  state: open
  title: thinking ot reasoning mode os different by model, default value gets errors
  url: https://api.github.com/repos/digiogithub/pando/issues/6
- number: 5
  state: open
  title: Error no auto compact in ACP mode
  url: https://api.github.com/repos/digiogithub/pando/issues/5
```

View a single issue by REST endpoint:

```bash
gh api repos/digiogithub/pando/issues/5
```

Filter only the essential fields:

```bash
gh api repos/digiogithub/pando/issues/5 --jq '{number, title, state, url, created_at}'
```

## 5. `yq` vs `jq`: same filtering, YAML output

`yq` is a jq-compatible wrapper that converts JSON to YAML. It is useful when you want the output to be more compact and readable in terminals or automation pipelines, and in many cases it reduces token usage versus verbose JSON.

Typical pattern:

```bash
gh issue list --repo digiogithub/pando --state open --limit 20 --json number,title,state,url | yq -y '.'
```

Another example over the REST API:

```bash
gh api repos/digiogithub/pando/issues --jq '.[] | select(.state=="open") | {number,title,state,url}' | yq -y '.'
```

Important note: in the `yq` wrapper installed in this environment, the YAML output flag is `-y`, not the jq `-P` flag. The intended usage is:

```bash
yq -y '.'
```

This means:

- `gh` performs the request and emits JSON
- `yq` parses it as jq-like input
- `-y` converts the result to YAML before printing

## 6. Common patterns for issue workflows

### List issues with filtering

```bash
gh issue list --repo digiogithub/pando --state open --search "ACP"
```

```bash
gh issue list --repo digiogithub/pando --state closed --limit 10
```

### Create a new issue

```bash
gh issue create --repo digiogithub/pando --title "Short summary" --body "Detailed description" --label bug
```

### Add a comment to an existing issue

```bash
gh issue comment 5 --repo digiogithub/pando --body "I am reviewing this issue and will provide a patch."
```

### Close or reopen an issue

```bash
gh issue close 5 --repo digiogithub/pando
```

```bash
gh issue reopen 5 --repo digiogithub/pando
```

## 7. Working directly with the GitHub API

`gh api` accepts GitHub REST endpoints using the repository context and can work from the current directory if the repo is already checked out:

```bash
gh api repos/{owner}/{repo}/issues
```

When inside the local repo, you can often omit the full repo name and rely on the current repository context:

```bash
gh api repos/{owner}/{repo}/issues
```

For explicit repository selection without changing directories:

```bash
gh api repos/digiogithub/pando/issues --jq '.[].number'
```

Useful flags:

- `--jq`: filter JSON output
- `--paginate`: page over multiple sets of results
- `-X METHOD`: override the HTTP method
- `-F` / `-f`: set fields or raw fields for POST requests
- `-H`: add custom headers
- `--input`: send a request body from a file
- `| yq -y '.'`: convert the JSON to YAML for lower token usage and more readable output

## 8. Typical usage for this project

From a local clone of `digiogithub/pando`, the exact commands to check open issues are:

```bash
cd /path/to/pando
gh issue list --repo digiogithub/pando --state open
```

Or, with YAML output:

```bash
gh api repos/digiogithub/pando/issues --jq '.[] | select(.state=="open") | {number,title,state}' | yq -y '.'
```

## 9. Verified commands for this repository

The following commands were executed successfully against the repo and produced the results shown above:

```bash
gh issue list --repo digiogithub/pando --state open --limit 20 --json number,title,state,url,author,createdAt | yq -y '.'
```

```bash
gh api repos/digiogithub/pando/issues --paginate --jq '.[] | select(.state=="open") | {number,title,state,url}' | yq -y '.'
```

## 10. Summary

The GitHub CLI is the most practical way to work with issues from the terminal:

- `gh issue list` for browsing issues
- `gh issue view` for single-issue inspection
- `gh issue comment` / `gh issue create` for issue management
- `gh api` for direct GitHub REST access and scripting
- `yq -y '.'` for YAML output with lower token overhead

For `digiogithub/pando`, the currently open issues are #5 and #6.
