# Docker runtime migration research: `github.com/docker/docker` → maintained Moby modules

## Context

Dependabot reported security issues against the Go dependency `github.com/docker/docker` used by the project. A direct upgrade to the Dependabot-patched version was investigated first.

## Key finding

A direct Go module upgrade to the version suggested by Dependabot is **not currently possible** for this project's dependency shape.

### Evidence

The project currently depends on:

- `github.com/docker/docker v28.5.2+incompatible`

Attempting to upgrade directly to the advisory-referenced version failed because that revision is not published as an installable Go module for `github.com/docker/docker`:

- `go get github.com/docker/docker@v29.3.1` → `invalid version: unknown revision v29.3.1`

Listing available module versions showed that the newest published version for this module path is still in the `v28.x+incompatible` line.

## Current usage in the codebase

The codebase uses `github.com/docker/docker` in exactly one project file:

- `internal/runtime/docker.go`

Imported packages there are:

- `github.com/docker/docker/api/types/container`
- `github.com/docker/docker/api/types/image`
- `github.com/docker/docker/client`
- `github.com/docker/docker/pkg/stdcopy`

This narrow usage makes the migration scope manageable.

## What the current runtime actually does

`internal/runtime/docker.go` implements Docker-backed session execution and uses the Docker Go client for:

1. Creating a Docker client
2. Listing images to determine whether the configured image already exists locally
3. Pulling an image when required by pull policy
4. Creating a container for a persistent session
5. Starting the container
6. Creating exec processes inside the running container
7. Attaching to exec output
8. Inspecting exec exit status
9. Listing containers by name to delete stale containers
10. Stopping, killing, and removing containers

The implementation does **not** call `docker cp` directly, but Dependabot still flags the dependency because advisories attach to the upstream Docker/Moby module surface.

## Safe migration target

The recommended migration target is the maintained split Moby modules:

- `github.com/moby/moby/client`
- `github.com/moby/moby/api`
- `github.com/moby/moby/api/pkg/stdcopy`

### Example currently available module versions observed during research

- `github.com/moby/moby/client v0.4.1`
- `github.com/moby/moby/api v1.54.2`

These modules are actively published and documented on pkg.go.dev.

## Important architectural finding

This migration is **not a pure import-path rename**.

The new Moby client separates:

- **client operations and option types** into `github.com/moby/moby/client`
- **core API structs** into `github.com/moby/moby/api/types/...`
- **stream multiplexing helpers** into `github.com/moby/moby/api/pkg/stdcopy`

## API differences that affect this project

### 1. Client construction changes

Current code uses:

- `client.NewClientWithOpts(...)`

Maintained Moby client prefers:

- `client.New(...)`

This part is low risk.

### 2. Option/request types move from `api/types/...` to `client`

This is the biggest migration detail.

The current code uses option structs from `github.com/docker/docker/api/types/...`, such as:

- `container.StartOptions`
- `container.ExecOptions`
- `container.ExecStartOptions`
- `container.RemoveOptions`
- `container.ListOptions`
- `image.ListOptions`
- `image.PullOptions`

In the maintained Moby client, these are represented as types in `github.com/moby/moby/client`, such as:

- `client.ContainerCreateOptions`
- `client.ContainerStartOptions`
- `client.ExecCreateOptions`
- `client.ExecAttachOptions`
- `client.ExecInspectOptions`
- `client.ContainerKillOptions`
- `client.ContainerRemoveOptions`
- `client.ContainerListOptions`
- `client.ImageListOptions`
- `client.ImagePullOptions`

### 3. Core container configuration structs remain in API types

The following concepts still belong to API types and remain familiar:

- `container.Config`
- `container.HostConfig`
- `container.NetworkMode`
- `container.Resources`

This reduces migration complexity because the core container configuration can remain structurally similar.

### 4. `stdcopy` import path changes

Current import:

- `github.com/docker/docker/pkg/stdcopy`

Migration target:

- `github.com/moby/moby/api/pkg/stdcopy`

The `StdCopy` helper still exists and remains suitable for multiplexed stdout/stderr demux in exec attach responses.

## Concrete impact by function in `internal/runtime/docker.go`

### `newDockerClient`

Current behavior:
- configures API version negotiation
- uses explicit socket, `DOCKER_HOST`, or `/var/run/docker.sock`

Migration impact:
- small
- replace `client.NewClientWithOpts` with `client.New`
- keep the same option logic if available through the Moby client options

### `StartSession`

Current behavior:
- ensures image exists
- computes security policy and resource limits
- removes stale container by generated name
- creates and starts session container

Migration impact:
- medium
- `ContainerCreate` signature changes to use `client.ContainerCreateOptions`
- `ContainerStart` changes to `client.ContainerStartOptions`
- core `container.Config` and `container.HostConfig` likely remain almost unchanged

### `Exec`

Current behavior:
- creates exec with shell command
- attaches to stdout/stderr
- demultiplexes stream with `stdcopy.StdCopy`
- inspects exit code

Migration impact:
- medium
- switch from `container.ExecOptions` to `client.ExecCreateOptions`
- switch from `container.ExecStartOptions` attach call to `client.ExecAttachOptions`
- switch inspect call to `client.ExecInspectOptions`
- returned attach type still exposes a hijacked response reader, so existing output flow should remain conceptually the same

### `StopSession`

Current behavior:
- stop container
- remove container
- ignore not-found errors

Migration impact:
- low
- update options types to `client.ContainerStopOptions` / `client.ContainerRemoveOptions`
- verify not-found error helpers remain available and equivalent

### `Kill`

Current behavior:
- sends `SIGKILL`

Migration impact:
- low
- use `client.ContainerKillOptions{Signal: "SIGKILL"}`

### `ensureImage`

Current behavior:
- lists local images
- compares `RepoTags`
- pull policy decides whether to pull

Migration impact:
- low to medium
- replace `image.ListOptions` with `client.ImageListOptions`
- verify returned image summary shape still exposes `RepoTags`

### `pullImage`

Current behavior:
- records events
- invokes `ImagePull`
- drains the response body to completion

Migration impact:
- low
- use `client.ImagePullOptions`
- the new client returns a richer pull response type, but draining it with `io.Copy(io.Discard, reader)` should remain valid

### `removeContainerIfExists`

Current behavior:
- lists all containers
- matches generated container name
- force removes stale container

Migration impact:
- low
- switch list/remove options to client-side option types

## Why this migration is considered safe

1. **Single-file usage surface**: only `internal/runtime/docker.go` needs refactoring.
2. **Core semantics unchanged**: the runtime logic remains the same: list/pull/create/start/exec/stop/remove.
3. **Container configuration structs remain familiar**: `container.Config` and `container.HostConfig` still exist in the API module.
4. **Stream handling remains supported**: `stdcopy.StdCopy` still exists in the Moby API module.
5. **Backward behavior can be preserved**: the refactor is largely mechanical if constrained to runtime adapter code.

## Risks and caveats

### 1. This does not fully replace Docker daemon patching

Some advisories flagged by Dependabot affect daemon behavior and operational security boundaries. Migrating the Go client away from the old monolithic module reduces dependency risk and technical debt, but it does **not** replace the need to:

- update Docker Engine / Moby daemon on hosts running Pando
- restrict Docker socket/API access
- avoid untrusted containers/images where relevant

### 2. API surface differences require careful compile-time refactoring

The split Moby modules moved request/option types into the client package, so the migration must be deliberate. Blind search/replace would be risky.

### 3. Error handling parity must be checked

The project currently uses helpers like `client.IsErrNotFound(err)`. Equivalent behavior should be confirmed after migration.

### 4. Runtime behavior should be tested against a real daemon

Unit tests alone are not sufficient. At least one real Docker-backed validation pass should be performed.

## Recommended migration plan

### Phase 1 — dependency switch

Update `go.mod` to:

- remove `github.com/docker/docker`
- add `github.com/moby/moby/client`
- add `github.com/moby/moby/api`

Also update import paths in `internal/runtime/docker.go`.

### Phase 2 — mechanical API refactor

Refactor `internal/runtime/docker.go` to:

- create the client via `client.New(...)`
- use `client.ContainerCreateOptions`
- use `client.ContainerStartOptions`
- use `client.ExecCreateOptions`
- use `client.ExecAttachOptions`
- use `client.ExecInspectOptions`
- use `client.ContainerKillOptions`
- use `client.ContainerRemoveOptions`
- use `client.ContainerListOptions`
- use `client.ImageListOptions`
- use `client.ImagePullOptions`
- use `github.com/moby/moby/api/pkg/stdcopy`

### Phase 3 — compile and test

Run at minimum:

- `go test ./internal/llm/agent ./internal/api`

And ideally add or run runtime-focused coverage for Docker-backed execution paths.

### Phase 4 — manual runtime validation

Validate with a real Docker daemon:

1. image already present locally
2. image pull required
3. `pull_policy=never`
4. explicit socket path
5. `DOCKER_HOST`
6. session start
7. command exec
8. stop and cleanup
9. kill path
10. stale container cleanup by generated name

## Recommendation

Proceed with a dedicated, focused refactor of `internal/runtime/docker.go` to the maintained Moby split modules.

This is the most realistic and technically sound way to address the Docker dependency situation, because:

- the direct `github.com/docker/docker` upgrade path suggested by Dependabot is not currently consumable as a Go module in this repo
- the current dependency usage is narrow and isolated
- the maintained Moby modules provide a supported migration target
- the refactor can preserve behavior if kept tightly scoped

## Summary

- A direct upgrade of `github.com/docker/docker` to the advisory-mentioned patch release is not currently possible in this repository.
- A safe migration path exists through `github.com/moby/moby/client`, `github.com/moby/moby/api`, and `github.com/moby/moby/api/pkg/stdcopy`.
- The migration is moderate in effort but low in blast radius because all usage is confined to `internal/runtime/docker.go`.
- The refactor should be done in a dedicated change and validated against a real Docker daemon.
