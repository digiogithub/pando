<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# I've been using jj vcs lately. It has functionality to set change points before starting changes and tracks everything better than git where you have to add files that didn't exist. How could I add similar functionality to jj vcs in a golang application without external dependencies?

Yes: the key idea of **jj** is not just "tracking better", but modeling work as a *working copy snapshot* associated with an active change, with auto-snapshot of new and deleted files, instead of relying on explicit staging like Git. In Go, without external dependencies, you can replicate this experience quite well if you design your app around "open changes" + automatic working tree snapshots.[^1_1][^1_2]

## What to copy from jj

In jj, the working copy belongs to a "working-copy commit", and the tool usually creates a new snapshot automatically when it detects changes; additionally, new files are implicitly tracked by default and deletions are reflected without manual `add`. There's also the idea of "opening a new change" to separate the next work, instead of accumulating everything in the same state.[^1_2][^1_1]

This can be translated to your app as:

- A current active change.
- A `new` or `begin` command that creates a base point before editing.[^1_1]
- A workspace scan that automatically detects additions, modifications and deletions.[^1_1]
- Ignore rules to not accidentally include temporary files or builds.[^1_1]


## Minimum design

The simplest architecture is to store a metadirectory, for example `.myvcs/`, with:

- `HEAD`: id of the active change.
- `changes/<id>.json`: change metadata, parents, timestamps, message.
- `objects/`: blobs by content hash.
- `snapshots/<id>.json`: map `path -> blobHash | tombstone`.
- `index-ignore`: simplified `.gitignore`-style rules.

Each "change" can be mutable, and each new snapshot replaces the previous snapshot of the active change, just like jj replaces the working copy revision with a newer one when it detects changes. If you want to get even closer to jj, separate the stable `change ID` from the immutable `snapshot ID`.[^1_3][^1_1]

## Recommended flow

The flow would be:

1. `begin "message"` creates an empty change with parent = current snapshot.[^1_1]
2. The user edits files freely.
3. Any relevant command (`status`, `diff`, `save`, `log`) first runs `snapshotWorkingCopy()`.[^1_1]
4. That snapshot compares disk vs previous snapshot of the active change and automatically updates additions, deletions and modifications.[^1_1]

Behavior example:

- You create `foo.go` and don't call `add`.
- You run `status`.
- Your app does a scan, calculates the file hash and puts it in the active snapshot automatically, like jj does with new files.[^1_2][^1_1]


## Implementation in Go

You can do it with only the standard library: `os`, `io`, `path/filepath`, `crypto/sha256`, `encoding/json`, `bufio`, `strings`, `time`. The central piece is building a working tree manifest and comparing it against the previous snapshot.

Simple structures:

```go
type Change struct {
	ID         string            `json:"id"`
	ParentID   string            `json:"parent_id"`
	Message    string            `json:"message"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	SnapshotID string            `json:"snapshot_id"`
}

type Snapshot struct {
	ID        string            `json:"id"`
	ChangeID  string            `json:"change_id"`
	Files     map[string]string `json:"files"` // path -> blob hash, "" => deleted
	CreatedAt time.Time         `json:"created_at"`
}
```

Snapshot pseudocode:

```go
func snapshotWorkingCopy(root string, current Snapshot, ignore func(string) bool) (Snapshot, error) {
	next := Snapshot{
		ID:        newID(),
		ChangeID:  current.ChangeID,
		Files:     map[string]string{},
		CreatedAt: time.Now(),
	}

	seen := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".myvcs" || strings.HasPrefix(rel, ".myvcs"+string(os.PathSeparator)) {
			return filepath.SkipDir
		}
		if ignore(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		hash, err := hashFile(path)
		if err != nil {
			return err
		}
		next.Files[filepath.ToSlash(rel)] = hash
		seen[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}

	for p := range current.Files {
		if !seen[p] {
			next.Files[p] = "" // tombstone
		}
	}

	return next, nil
}
```

The blob hash can be SHA-256, and if the blob doesn't exist yet in `objects/`, you write it once. That gives you basic deduplication without any external dependencies.

## Important decisions

If you want it to really feel like jj, I recommend these decisions:

- Do auto-snapshot before almost any command, not just on `commit`, because jj updates the working copy state when running many commands.[^1_1]
- Consider new files as tracked by default, unless ignore match.[^1_2][^1_1]
- Model deletions as tombstones in the snapshot, not as silent absence, to be able to calculate diffs properly.
- Add `untrack <path>` to exclude a file while keeping it on disk, similar to the behavior documented by jj.[^1_1]

And if you want something pragmatic for a business app, you don't need to implement a complete VCS:

- Keep a single active change per workspace.
- Store whole snapshots for simplicity.
- Generate diffs on the fly by comparing two `path -> hash` maps.
- Leave merge/rebase for a later phase.

If you want, I can prepare a functional prototype in Go for you next, without external dependencies, with `init`, `begin`, `status`, `save` commands and auto-tracking of new files.
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15][^1_4][^1_5][^1_6][^1_7][^1_8][^1_9]</span>

<div align="center">⁂</div>

[^1_1]: https://gist.github.com/christianromney/27fd1fca9e5f24ef24d9ed6c9eddda50

[^1_2]: https://swiftwithmajid.com/2025/10/22/introducing-jujutsu-vcs-edit-workflow/

[^1_3]: https://kubamartin.com/posts/introduction-to-the-jujutsu-vcs/

[^1_4]: https://www.reddit.com/r/programming/comments/1hentx8/tech_notes_the_jujutsu_version_control_system/

[^1_5]: https://danverbraganza.com/writings/most-frequent-jj-commands

[^1_6]: https://jj-vcs.github.io/jj/v0.17.0/working-copy/

[^1_7]: https://stackoverflow.com/questions/17078727/what-is-the-most-effective-way-to-lock-down-external-dependency-versions-in-go

[^1_8]: https://github.com/jj-vcs/jj

[^1_9]: https://docs.jj-vcs.dev/latest/working-copy/

[^1_10]: https://news.ycombinator.com/item?id=43021515

[^1_11]: https://swiftwithmajid.com/2025/10/15/introducing-jujutsu-vcs/

[^1_12]: https://github.com/martinvonz/jj/blob/main/docs/working-copy.md

[^1_13]: https://www.x-cmd.com/install/jj/

[^1_14]: https://docs.jj-vcs.dev/latest/config/

[^1_15]: https://www.jj-vcs.dev/v0.15.1/working-copy/


---

# how could I have my own jj implementation so that each AI session works on a different branch or file version that I can then incorporate by default, or discard the changes, give me the code start

Yes: for your case you don't need to clone all of jj, but build an **ephemeral workspaces engine** where each AI session has its own mutable change on a common base, and then you can `apply` or `discard` that session. In jj, the working copy is a mutable snapshot that auto-updates and changes are organized as editable revisions; that idea fits very well with "one AI session = one isolated branch/change".[^2_1][^2_2][^2_3]

## Mental model

The most practical way is to think in three layers:

- `base snapshot`: the current approved state.
- `session snapshot`: the changes of a specific AI session.
- `overlay materialized`: the files that session sees on disk or in memory.[^2_3][^2_1]

Instead of Git branches, each session can point to `BaseID + own changes`, and when it finishes you do one of two things: incorporate the session snapshot into the base, or abandon it. In jj, editing mutable changes and relocating work afterwards is part of the normal flow; here you'd use the same pattern, but oriented to agent sessions.[^2_4][^2_3]

## Simple design

Create a directory like `.aijj/` with this structure:

- `objects/`: blobs by hash.
- `snapshots/`: maps `path -> blobHash`.
- `sessions/`: metadata of each session.
- `HEAD`: current base snapshot.[^2_1]

Minimum types in Go:

```go
package aijj

import "time"

type SnapshotID string
type SessionID string
type BlobID string

type Snapshot struct {
	ID        SnapshotID        `json:"id"`
	Parent    SnapshotID        `json:"parent"`
	Files     map[string]BlobID `json:"files"` // path -> blob
	CreatedAt time.Time         `json:"created_at"`
	Message   string            `json:"message"`
}

type Session struct {
	ID             SessionID  `json:"id"`
	Name           string     `json:"name"`
	BaseSnapshotID SnapshotID `json:"base_snapshot_id"`
	HeadSnapshotID SnapshotID `json:"head_snapshot_id"`
	Status         string     `json:"status"` // open, applied, discarded
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
```

The important decision is that a session doesn't "own" complete files, but a `HeadSnapshotID` that represents its current version. This way you can create many sessions from the same base without duplicating too much state.[^2_5][^2_6]

## Session flow

The minimum flow would be:

1. `Init(repoRoot)` creates `.aijj/` and an empty or initial base snapshot.
2. `NewSession(name)` creates a new session pointing to the current `HEAD`.
3. The AI writes files in a session workspace or via API.
4. `SaveSession(sessionID)` rescans files and generates a new snapshot.
5. `ApplySession(sessionID)` converts that snapshot into the new base.
6. `DiscardSession(sessionID)` marks the session as discarded.[^2_2][^2_1]

If several sessions start from the same base, you can start with a simple policy: only allow `apply` if the base hasn't changed since the session was born. If it changed, return conflict and later implement three-way merge. That restriction simplifies the beginning a lot.[^2_7]

## Code start

Below you have a functional starting base, without external dependencies, focused on creating sessions, saving snapshots and applying or discarding. It's designed to later evolve towards diff, merge and materialized workspaces.

```go
package aijj

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SnapshotID string
type SessionID string
type BlobID string

type Snapshot struct {
	ID        SnapshotID        `json:"id"`
	Parent    SnapshotID        `json:"parent"`
	Files     map[string]BlobID `json:"files"`
	CreatedAt time.Time         `json:"created_at"`
	Message   string            `json:"message"`
}

type Session struct {
	ID             SessionID  `json:"id"`
	Name           string     `json:"name"`
	BaseSnapshotID SnapshotID `json:"base_snapshot_id"`
	HeadSnapshotID SnapshotID `json:"head_snapshot_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Repo struct {
	Root string
	Meta string
}

func Init(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	dirs := []string{
		meta,
		filepath.Join(meta, "objects"),
		filepath.Join(meta, "snapshots"),
		filepath.Join(meta, "sessions"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	r := &Repo{Root: root, Meta: meta}

	if _, err := os.Stat(filepath.Join(meta, "HEAD")); errors.Is(err, os.ErrNotExist) {
		snap := Snapshot{
			ID:        SnapshotID(newID("snap")),
			Parent:    "",
			Files:     map[string]BlobID{},
			CreatedAt: time.Now().UTC(),
			Message:   "initial",
		}
		if err := r.saveSnapshot(snap); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte(snap.ID), 0o644); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func Open(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	if _, err := os.Stat(meta); err != nil {
		return nil, err
	}
	return &Repo{Root: root, Meta: meta}, nil
}

func (r *Repo) NewSession(name string) (Session, error) {
	head, err := r.Head()
	if err != nil {
		return Session{}, err
	}

	now := time.Now().UTC()
	s := Session{
		ID:             SessionID(newID("sess")),
		Name:           name,
		BaseSnapshotID: head.ID,
		HeadSnapshotID: head.ID,
		Status:         "open",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.saveSession(s); err != nil {
		return Session{}, err
	}
	return s, nil
}

func (r *Repo) Head() (Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(r.Meta, "HEAD"))
	if err != nil {
		return Snapshot{}, err
	}
	return r.loadSnapshot(SnapshotID(strings.TrimSpace(string(b))))
}

func (r *Repo) LoadSession(id SessionID) (Session, error) {
	var s Session
	err := readJSON(filepath.Join(r.Meta, "sessions", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) SaveSessionFromDir(id SessionID, dir string, message string) (Snapshot, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	if sess.Status != "open" {
		return Snapshot{}, errors.New("session is not open")
	}

	parent, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return Snapshot{}, err
	}

	files, err := r.captureDir(dir)
	if err != nil {
		return Snapshot{}, err
	}

	if sameFiles(parent.Files, files) {
		return parent, nil
	}

	next := Snapshot{
		ID:        SnapshotID(newID("snap")),
		Parent:    parent.ID,
		Files:     files,
		CreatedAt: time.Now().UTC(),
		Message:   message,
	}
	if err := r.saveSnapshot(next); err != nil {
		return Snapshot{}, err
	}

	sess.HeadSnapshotID = next.ID
	sess.UpdatedAt = time.Now().UTC()
	if err := r.saveSession(sess); err != nil {
		return Snapshot{}, err
	}

	return next, nil
}

func (r *Repo) ApplySession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}

	currentHead, err := r.Head()
	if err != nil {
		return err
	}
	if currentHead.ID != sess.BaseSnapshotID {
		return errors.New("base changed; merge required")
	}

	if err := os.WriteFile(filepath.Join(r.Meta, "HEAD"), []byte(sess.HeadSnapshotID), 0o644); err != nil {
		return err
	}

	sess.Status = "applied"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) DiscardSession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}
	sess.Status = "discarded"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) MaterializeSnapshot(id SnapshotID, dst string) error {
	snap, err := r.loadSnapshot(id)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for path, blob := range snap.Files {
		target := filepath.Join(dst, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(r.Meta, "objects", string(blob)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) captureDir(dir string) (map[string]BlobID, error) {
	files := map[string]BlobID{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}
		if rel == ".aijj" || strings.HasPrefix(rel, ".aijj/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		blob, err := r.storeBlob(path)
		if err != nil {
			return err
		}
		files[rel] = blob
		return nil
	})

	return files, err
}

func (r *Repo) storeBlob(path string) (BlobID, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	blob := BlobID(sum)

	objPath := filepath.Join(r.Meta, "objects", sum)
	if _, err := os.Stat(objPath); errors.Is(err, os.ErrNotExist) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(objPath, data, 0o644); err != nil {
			return "", err
		}
	}
	return blob, nil
}

func (r *Repo) saveSnapshot(s Snapshot) error {
	return writeJSON(filepath.Join(r.Meta, "snapshots", string(s.ID)+".json"), s)
}

func (r *Repo) loadSnapshot(id SnapshotID) (Snapshot, error) {
	var s Snapshot
	err := readJSON(filepath.Join(r.Meta, "snapshots", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) saveSession(s Session) error {
	return writeJSON(filepath.Join(r.Meta, "sessions", string(s.ID)+".json"), s)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func sameFiles(a, b map[string]BlobID) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}

func newID(prefix string) string {
	now := time.Now().UTC().Format("20060102T150405.000000000")
	return prefix + "_" + now
}

func (r *Repo) DiffSession(id SessionID) ([]string, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return nil, err
	}
	base, err := r.loadSnapshot(sess.BaseSnapshotID)
	if err != nil {
		return nil, err
	}
	head, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return nil, err
	}

	pathsMap := map[string]struct{}{}
	for p := range base.Files {
		pathsMap[p] = struct{}{}
	}
	for p := range head.Files {
		pathsMap[p] = struct{}{}
	}

	paths := make([]string, 0, len(pathsMap))
	for p := range pathsMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []string
	for _, p := range paths {
		bv, bok := base.Files[p]
		hv, hok := head.Files[p]
		switch {
		case !bok && hok:
			out = append(out, "A "+p)
		case bok && !hok:
			out = append(out, "D "+p)
		case bok && hok && bv != hv:
			out = append(out, "M "+p)
		}
	}
	return out, nil
}
```


## How to use it

The cleanest way is for each AI session to work in its own materialized directory, for example:

- `sessions/agent-1/`
- `sessions/agent-2/`
- `sessions/reviewer/`

You create the session, materialize its snapshot, let the agent write there, and then call `SaveSessionFromDir()`. If you like the result, you do `ApplySession()`; if not, `DiscardSession()`. This imitates jj's idea of editable changes and automatic snapshots quite well, but simplified for your case.[^2_2][^2_3][^2_1]

What I would implement next is:

- `ignore patterns`
- `three-way merge`
- content `diff`
- session `checkout`
- `locks` for concurrency between agents

If you want, in the next message I'll prepare a **complete CLI in Go** (`init`, `session new`, `session save`, `session diff`, `session apply`, `session discard`) on this base.
<span style="display:none">[^2_10][^2_11][^2_12][^2_13][^2_14][^2_15][^2_8][^2_9]</span>

<div align="center">⁂</div>

[^2_1]: https://docs.jj-vcs.dev/latest/working-copy/

[^2_2]: https://man.archlinux.org/man/extra/jujutsu/jj-file-show.1.en

[^2_3]: https://kubamartin.com/posts/introduction-to-the-jujutsu-vcs/

[^2_4]: https://swiftwithmajid.com/2025/10/22/introducing-jujutsu-vcs-edit-workflow/

[^2_5]: https://neugierig.org/software/blog/2024/12/jujutsu.html

[^2_6]: https://www.stavros.io/posts/switch-to-jujutsu-already-a-tutorial/

[^2_7]: https://docs.jj-vcs.dev/v0.16.0/branches/

[^2_8]: https://github.com/jj-vcs/jj/issues/2876

[^2_9]: https://github.com/jj-vcs/jj/discussions/2425

[^2_10]: https://danverbraganza.com/writings/most-frequent-jj-commands

[^2_11]: https://docs.jj-vcs.dev/latest/cli-reference/

[^2_12]: https://news.ycombinator.com/item?id=43020180

[^2_13]: https://www.youtube.com/watch?v=ou4ZNRFXkO0

[^2_14]: https://www.reddit.com/r/git/comments/1nf74iz/another_jujutsu_post/

[^2_15]: https://gist.github.com/christianromney/27fd1fca9e5f24ef24d9ed6c9eddda50


---

# go ahead, prepare a complete cli in go

Yes: below I leave you a complete CLI in Go, without external dependencies, with subcommands using `flag.FlagSet`, which is the standard approach for CLIs with subcommands in the standard library. The idea follows jj's model of working on a mutable working copy/snapshot, but adapted to "AI sessions" that you can apply or discard.[^3_1][^3_2][^3_3][^3_4]

## Structure

I propose this minimal layout:

- `go.mod`
- `main.go`
- `aijj/repo.go`
- `aijj/cli.go`

With this you'll have:

- `aijj init`
- `aijj session new`
- `aijj session list`
- `aijj session materialize`
- `aijj session save`
- `aijj session diff`
- `aijj session apply`
- `aijj session discard`
- `aijj head`[^3_5][^3_1]


## go.mod

```go
module github.com/youruser/aijj

go 1.22
```


## aijj/repo.go

```go
package aijj

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SnapshotID string
type SessionID string
type BlobID string

type Snapshot struct {
	ID        SnapshotID        `json:"id"`
	Parent    SnapshotID        `json:"parent"`
	Files     map[string]BlobID `json:"files"`
	CreatedAt time.Time         `json:"created_at"`
	Message   string            `json:"message"`
}

type Session struct {
	ID             SessionID  `json:"id"`
	Name           string     `json:"name"`
	BaseSnapshotID SnapshotID `json:"base_snapshot_id"`
	HeadSnapshotID SnapshotID `json:"head_snapshot_id"`
	Status         string     `json:"status"`
	Workspace      string     `json:"workspace"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Repo struct {
	Root string
	Meta string
}

func Init(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	dirs := []string{
		meta,
		filepath.Join(meta, "objects"),
		filepath.Join(meta, "snapshots"),
		filepath.Join(meta, "sessions"),
		filepath.Join(meta, "workspaces"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	r := &Repo{Root: root, Meta: meta}

	if _, err := os.Stat(filepath.Join(meta, "HEAD")); errors.Is(err, os.ErrNotExist) {
		snap := Snapshot{
			ID:        SnapshotID(newID("snap")),
			Parent:    "",
			Files:     map[string]BlobID{},
			CreatedAt: time.Now().UTC(),
			Message:   "initial",
		}
		if err := r.saveSnapshot(snap); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte(snap.ID), 0o644); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func Open(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	if _, err := os.Stat(meta); err != nil {
		return nil, err
	}
	return &Repo{Root: root, Meta: meta}, nil
}

func (r *Repo) Head() (Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(r.Meta, "HEAD"))
	if err != nil {
		return Snapshot{}, err
	}
	return r.loadSnapshot(SnapshotID(strings.TrimSpace(string(b))))
}

func (r *Repo) NewSession(name string) (Session, error) {
	head, err := r.Head()
	if err != nil {
		return Session{}, err
	}

	now := time.Now().UTC()
	id := SessionID(newID("sess"))
	workspace := filepath.Join(r.Meta, "workspaces", string(id))

	s := Session{
		ID:             id,
		Name:           name,
		BaseSnapshotID: head.ID,
		HeadSnapshotID: head.ID,
		Status:         "open",
		Workspace:      workspace,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.saveSession(s); err != nil {
		return Session{}, err
	}
	return s, nil
}

func (r *Repo) ListSessions() ([]Session, error) {
	dir := filepath.Join(r.Meta, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var s Session
		if err := readJSON(filepath.Join(dir, e.Name()), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *Repo) LoadSession(id SessionID) (Session, error) {
	var s Session
	err := readJSON(filepath.Join(r.Meta, "sessions", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) MaterializeSession(id SessionID) (string, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return "", err
	}
	if sess.Status != "open" {
		return "", errors.New("session is not open")
	}

	if err := os.RemoveAll(sess.Workspace); err != nil {
		return "", err
	}
	if err := os.MkdirAll(sess.Workspace, 0o755); err != nil {
		return "", err
	}

	if err := r.MaterializeSnapshot(sess.HeadSnapshotID, sess.Workspace); err != nil {
		return "", err
	}
	return sess.Workspace, nil
}

func (r *Repo) SaveSession(id SessionID, message string) (Snapshot, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	return r.SaveSessionFromDir(sess.ID, sess.Workspace, message)
}

func (r *Repo) SaveSessionFromDir(id SessionID, dir string, message string) (Snapshot, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	if sess.Status != "open" {
		return Snapshot{}, errors.New("session is not open")
	}

	parent, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return Snapshot{}, err
	}

	files, err := r.captureDir(dir)
	if err != nil {
		return Snapshot{}, err
	}

	if sameFiles(parent.Files, files) {
		return parent, nil
	}

	next := Snapshot{
		ID:        SnapshotID(newID("snap")),
		Parent:    parent.ID,
		Files:     files,
		CreatedAt: time.Now().UTC(),
		Message:   message,
	}
	if err := r.saveSnapshot(next); err != nil {
		return Snapshot{}, err
	}

	sess.HeadSnapshotID = next.ID
	sess.UpdatedAt = time.Now().UTC()
	if err := r.saveSession(sess); err != nil {
		return Snapshot{}, err
	}
	return next, nil
}

func (r *Repo) ApplySession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}

	currentHead, err := r.Head()
	if err != nil {
		return err
	}
	if currentHead.ID != sess.BaseSnapshotID {
		return errors.New("base changed; merge required")
	}

	if err := os.WriteFile(filepath.Join(r.Meta, "HEAD"), []byte(sess.HeadSnapshotID), 0o644); err != nil {
		return err
	}

	sess.Status = "applied"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) DiscardSession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}
	sess.Status = "discarded"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) DiffSession(id SessionID) ([]string, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return nil, err
	}

	base, err := r.loadSnapshot(sess.BaseSnapshotID)
	if err != nil {
		return nil, err
	}
	head, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return nil, err
	}

	pathsMap := map[string]struct{}{}
	for p := range base.Files {
		pathsMap[p] = struct{}{}
	}
	for p := range head.Files {
		pathsMap[p] = struct{}{}
	}

	paths := make([]string, 0, len(pathsMap))
	for p := range pathsMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []string
	for _, p := range paths {
		bv, bok := base.Files[p]
		hv, hok := head.Files[p]
		switch {
		case !bok && hok:
			out = append(out, "A "+p)
		case bok && !hok:
			out = append(out, "D "+p)
		case bok && hok && bv != hv:
			out = append(out, "M "+p)
		}
	}
	return out, nil
}

func (r *Repo) MaterializeSnapshot(id SnapshotID, dst string) error {
	snap, err := r.loadSnapshot(id)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for path, blob := range snap.Files {
		target := filepath.Join(dst, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(r.Meta, "objects", string(blob)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) captureDir(dir string) (map[string]BlobID, error) {
	files := map[string]BlobID{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		blob, err := r.storeBlob(path)
		if err != nil {
			return err
		}
		files[rel] = blob
		return nil
	})

	return files, err
}

func (r *Repo) storeBlob(path string) (BlobID, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	blob := BlobID(sum)

	objPath := filepath.Join(r.Meta, "objects", sum)
	if _, err := os.Stat(objPath); errors.Is(err, os.ErrNotExist) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(objPath, data, 0o644); err != nil {
			return "", err
		}
	}
	return blob, nil
}

func (r *Repo) saveSnapshot(s Snapshot) error {
	return writeJSON(filepath.Join(r.Meta, "snapshots", string(s.ID)+".json"), s)
}

func (r *Repo) loadSnapshot(id SnapshotID) (Snapshot, error) {
	var s Snapshot
	err := readJSON(filepath.Join(r.Meta, "snapshots", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) saveSession(s Session) error {
	return writeJSON(filepath.Join(r.Meta, "sessions", string(s.ID)+".json"), s)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func sameFiles(a, b map[string]BlobID) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}

func newID(prefix string) string {
	return prefix + "_" + time.Now().UTC().Format("20060102T150405.000000000")
}
```


## aijj/cli.go

```go
package aijj

import (
	"flag"
	"fmt"
	"io"
)

type CLI struct {
	Stdout io.Writer
	Stderr io.Writer
}

func NewCLI(stdout, stderr io.Writer) *CLI {
	return &CLI{Stdout: stdout, Stderr: stderr}
}

func (c *CLI) Run(args []string) int {
	if len(args) == 0 {
		c.usage()
		return 1
	}

	switch args[^3_0] {
	case "init":
		return c.runInit(args[1:])
	case "head":
		return c.runHead(args[1:])
	case "session":
		return c.runSession(args[1:])
	case "help", "-h", "--help":
		c.usage()
		return 0
	default:
		fmt.Fprintf(c.Stderr, "unknown command: %s\n", args[^3_0])
		c.usage()
		return 1
	}
}

func (c *CLI) usage() {
	fmt.Fprintln(c.Stdout, "aijj - snapshots per AI session")
	fmt.Fprintln(c.Stdout, "")
	fmt.Fprintln(c.Stdout, "Usage:")
	fmt.Fprintln(c.Stdout, "  aijj init [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj head [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session new -name NAME [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session list [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session materialize -id SESSION_ID [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session save -id SESSION_ID [-m MESSAGE] [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session diff -id SESSION_ID [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session apply -id SESSION_ID [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session discard -id SESSION_ID [-repo PATH]")
}

func (c *CLI) runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r, err := Init(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "init error: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.Stdout, "repo initialized: %s\n", r.Meta)
	return 0
}

func (c *CLI) runHead(args []string) int {
	fs := flag.NewFlagSet("head", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	head, err := r.Head()
	if err != nil {
		fmt.Fprintf(c.Stderr, "head error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "HEAD: %s\n", head.ID)
	fmt.Fprintf(c.Stdout, "Parent: %s\n", head.Parent)
	fmt.Fprintf(c.Stdout, "Message: %s\n", head.Message)
	fmt.Fprintf(c.Stdout, "Files: %d\n", len(head.Files))
	return 0
}

func (c *CLI) runSession(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.Stderr, "missing session subcommand")
		return 1
	}

	switch args[^3_0] {
	case "new":
		return c.runSessionNew(args[1:])
	case "list":
		return c.runSessionList(args[1:])
	case "materialize":
		return c.runSessionMaterialize(args[1:])
	case "save":
		return c.runSessionSave(args[1:])
	case "diff":
		return c.runSessionDiff(args[1:])
	case "apply":
		return c.runSessionApply(args[1:])
	case "discard":
		return c.runSessionDiscard(args[1:])
	default:
		fmt.Fprintf(c.Stderr, "unknown session subcommand: %s\n", args[^3_0])
		return 1
	}
}

func (c *CLI) runSessionNew(args []string) int {
	fs := flag.NewFlagSet("session new", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	name := fs.String("name", "", "session name")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" {
		fmt.Fprintln(c.Stderr, "missing -name")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	s, err := r.NewSession(*name)
	if err != nil {
		fmt.Fprintf(c.Stderr, "new session error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Session: %s\n", s.ID)
	fmt.Fprintf(c.Stdout, "Name: %s\n", s.Name)
	fmt.Fprintf(c.Stdout, "Base: %s\n", s.BaseSnapshotID)
	fmt.Fprintf(c.Stdout, "Workspace: %s\n", s.Workspace)
	return 0
}

func (c *CLI) runSessionList(args []string) int {
	fs := flag.NewFlagSet("session list", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	sessions, err := r.ListSessions()
	if err != nil {
		fmt.Fprintf(c.Stderr, "list sessions error: %v\n", err)
		return 1
	}

	for _, s := range sessions {
		fmt.Fprintf(c.Stdout, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Status, s.Name, s.BaseSnapshotID, s.HeadSnapshotID)
	}
	return 0
}

func (c *CLI) runSessionMaterialize(args []string) int {
	fs := flag.NewFlagSet("session materialize", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	workspace, err := r.MaterializeSession(SessionID(*id))
	if err != nil {
		fmt.Fprintf(c.Stderr, "materialize error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Workspace ready: %s\n", workspace)
	return 0
}

func (c *CLI) runSessionSave(args []string) int {
	fs := flag.NewFlagSet("session save", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	id := fs.String("id", "", "session id")
	msg := fs.String("m", "session save", "snapshot message")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	snap, err := r.SaveSession(SessionID(*id), *msg)
	if err != nil {
		fmt.Fprintf(c.Stderr, "save error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Saved snapshot: %s\n", snap.ID)
	fmt.Fprintf(c.Stdout, "Parent: %s\n", snap.Parent)
	fmt.Fprintf(c.Stdout, "Files: %d\n", len(snap.Files))
	return 0
}

func (c *CLI) runSessionDiff(args []string) int {
	fs := flag.NewFlagSet("session diff", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	diff, err := r.DiffSession(SessionID(*id))
	if err != nil {
		fmt.Fprintf(c.Stderr, "diff error: %v\n", err)
		return 1
	}

	if len(diff) == 0 {
		fmt.Fprintln(c.Stdout, "No changes")
		return 0
	}

	for _, line := range diff {
		fmt.Fprintln(c.Stdout, line)
	}
	return 0
}

func (c *CLI) runSessionApply(args []string) int {
	fs := flag.NewFlagSet("session apply", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	if err := r.ApplySession(SessionID(*id)); err != nil {
		fmt.Fprintf(c.Stderr, "apply error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Session applied: %s\n", *id)
	return 0
}

func (c *CLI) runSessionDiscard(args []string) int {
	fs := flag.NewFlagSet("session discard", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "repository path")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	if err := r.DiscardSession(SessionID(*id)); err != nil {
		fmt.Fprintf(c.Stderr, "discard error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Session discarded: %s\n", *id)
	return 0
}
```


## main.go

```go
package main

import (
	"os"

	"github.com/youruser/aijj/aijj"
)

func main() {
	cli := aijj.NewCLI(os.Stdout, os.Stderr)
	os.Exit(cli.Run(os.Args[1:]))
}
```


## Real usage

With this you already have a quite useful flow for AI agents that work isolated on snapshots, something conceptually close to jj's editable working copy.[^3_3][^3_4]

Example:

```bash
go build -o aijj .

./aijj init
./aijj session new -name "agent-refactor"
./aijj session list
./aijj session materialize -id sess_20260315T225200.000000000

# the AI writes in .aijj/workspaces/<session-id>/

./aijj session save -id sess_20260315T225200.000000000 -m "refactor auth middleware"
./aijj session diff -id sess_20260315T225200.000000000
./aijj session apply -id sess_20260315T225200.000000000
```


## What I would improve now

For production, I would add these pieces first:

- `.aijjignore`, to not snapshot binaries, caches or `.git/`.
- Repository lock, so two agents don't `apply` at the same time.
- Three-way merge, because now `apply` fails if the base changed in between.
- Per-file content diff, because now it only reports `A/M/D`.
- A `session exec` command to directly launch an agent in the materialized workspace.[^3_6][^3_1]

If you want, the next step I'll give you ready-made: a `Makefile`, basic tests and an improvement so that `session save` automatically detects deletions, ignores and application conflicts.
<span style="display:none">[^3_10][^3_11][^3_12][^3_13][^3_14][^3_15][^3_7][^3_8][^3_9]</span>

<div align="center">⁂</div>

[^3_1]: https://gobyexample.com/command-line-subcommands

[^3_2]: https://www.digitalocean.com/community/tutorials/how-to-use-the-flag-package-in-go

[^3_3]: https://kristofferbalintona.me/posts/202503270335/

[^3_4]: https://docs.jj-vcs.dev/latest/working-copy/

[^3_5]: https://abhinavg.net/2022/08/13/flag-subcommand/

[^3_6]: https://appliedgo.net/spotlight/subcommands-without-command-package/

[^3_7]: https://www.reddit.com/r/golang/comments/1h18wi4/golang_flags_subcommands_and_sourcefiles_per/

[^3_8]: https://www.bytesizego.com/blog/golang-flag-package

[^3_9]: https://stackoverflow.com/questions/67904765/global-flags-and-subcommands

[^3_10]: https://github.com/arran4/go-subcommand

[^3_11]: https://osinet.fr/go/en/articles/cli-google-subcommands/

[^3_12]: https://docs.jj-vcs.dev/latest/github/

[^3_13]: https://stackoverflow.com/questions/67642066/providing-context-to-cobra-subcommands-without-using-package-globals

[^3_14]: https://github.com/cosiner/flag

[^3_15]: https://github.com/jj-vcs/jj/blob/main/docs/git-comparison.md
