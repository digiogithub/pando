# Implementation Plan: Container Runtime and File Access Support for Pando

## Executive Summary
Pando already has three useful abstraction points for introducing containers without rewriting the entire architecture: 1) the `bash` tool already separates local execution from remote execution via ACP (`internal/llm/tools/bash.go`), 2) `view` and `write` already support an alternative path through `ACPClientConnection` (`internal/llm/tools/view.go`, `internal/llm/tools/write.go`), and 3) Mesnada ACP already implements workspace validation, capabilities and lifecycle for terminals and files (`internal/mesnada/acp/client.go`, `internal/mesnada/acp/client_connection.go`, `internal/mesnada/acp/security_test.go`).

The opportunity is to introduce a common runtime/FS contract so that `bash`, `view`, `write`, `edit` and `patch` can run on: current host, Docker, Podman and later on a custom embedded runtime. The recommended strategy is to start with bind-mount of the workspace inside the container to minimize impact on history, permissions, locking, LSP and paths, and only later evolve toward a native OCI runtime with pull/cache from registry.

Additionally, container execution capability must be configurable in a homogeneous way from CLI/config file, TUI and Web UI/API, with auto-detection of Docker, Podman or Pando's native embedded runtime, and explicit support for both container execution and Docker/OCI image usage and download from registry.

## Codebase Findings
- `internal/llm/tools/bash.go`: executes persistent local shell via `shell.GetPersistentShell(config.WorkingDirectory())`; in ACP uses `CreateTerminal`, `WaitForTerminalExit` and `TerminalOutput`.
- `internal/llm/tools/shell/shell.go`: maintains a persistent process-level shell session with mutable cwd, timeout and cancellation.
- `internal/llm/tools/view.go`: direct local read with `os.Stat`/`os.Open`/`os.ReadFile`; ACP alternative with `ReadTextFile`.
- `internal/llm/tools/write.go`: local write with `modTime` validation, history (`internal/history`), permissions (`internal/permission`) and LSP diagnostics; ACP alternative with `WriteTextFile`.
- `internal/llm/tools/edit.go` and `internal/llm/tools/patch.go`: heavily depend on local FS access, timestamps, locks and current workspace content.
- `internal/mesnada/acp/client.go`: already defines workspace boundary, terminal and file access capabilities, and reusable security test patterns.
- `internal/config/config.go`: currently only `ShellConfig` and `BashConfig` exist; no container runtime configuration.
- `internal/api/handlers_config.go`: API surface for configuration already exists; good entry point for exposing runtime settings to Web UI.
- `internal/llm/agent/tools.go`: current tool wiring allows injecting new implementations without changing the agent's external contract.

## Recommended Architectural Decisions
1. **Do not couple tools directly to Docker/Podman**. Create a `ContainerRuntime`/`WorkspaceFS` abstraction and have tools depend on it.
2. **Keep `host` as the default runtime** to avoid breaking current sessions.
3. **Use bind mount of the workspace as the first step** so that host paths remain real and history/LSP/permissions work with minimal changes.
4. **Separate command execution from file access**: a runtime may support exec but not virtual FS, or vice versa.
5. **Session/project persistence**: the bash tool needs persistent semantics; a persistent container per session is preferable to one per command, at least for bash.
6. **Minimal network and privileges by default**: `network=none`, non-root user, read-only rootfs and RW mount only for the workspace when viable.
7. **Auto-detection first, explicit selection second**: Pando must discover which backends are installed and then allow the user to choose `docker`, `podman`, `embedded` or `host` from config, TUI and Web UI.
8. **Embedded runtime after the contract is established**: first stabilize the interface with Docker/Podman, then implement a custom backend.

## Main Risks
- `shell/shell.go` assumes a single global persistent shell; for containers it will need to be indexed by session+runtime+workspace.
- `write/edit/patch` currently depend on `os.Stat`, `os.ReadFile`, `os.WriteFile` and real host modtimes; if the FS is no longer bind-mounted, consistency will need to be redesigned.
- `bash.go` in ACP does simple parsing with `strings.Fields`; for containers this may be insufficient if the command is sent to an `exec` API with argv.
- Docker and Podman do not expose exactly the same experience: Docker typically uses daemon/socket; Podman is typically rootless and via REST socket. It is better to normalize capabilities, not APIs.
- An embedded runtime with pull from registry implies resolving image store, OCI formats, unpack, GC, authentication, verification and host isolation: this is clearly a later initiative.
- UX may become fragmented if config/API/TUI/Web UI do not share the same runtime, image, auto-detection and fallback model.

## Phases

### Phase 1 — Discovery and Base Abstraction
**fact_id:** 4

Objective: introduce interfaces, engine auto-detection and the configuration model without yet changing the default behavior.

Deliverables:
- New interfaces, for example:
  - `CommandRuntime` or `ExecutionRuntime` for `Exec`, `StartSession`, `StopSession`, `Output`, `Kill`
  - `WorkspaceFS` for `ReadFile`, `WriteFile`, `Stat`, `MkdirAll`, `Remove`, `List`
  - `RuntimeResolver` for selecting `host|docker|podman|embedded`
- New configuration in `internal/config/config.go` and persistence in configuration file (`.toml` and the representation used by API/Web UI; if a `.js` variant exists, map it too) for runtime, image, pull policy, socket/endpoint, mounts, network and resources.
- Engine auto-detection model: detect Docker, Podman or Pando's native embedded runtime and expose that capability to the rest of the system.
- Minimum refactoring in `bash/view/write` to depend on the resolved runtime/FS.
- Keep `host` as the default implementation using current logic.

Suggested changes:
- Create a new package, for example `internal/runtime` or `internal/containers`.
- Move the local persistent shell logic to a `hostRuntime` implementation.
- Add context/metadata keys to know which runtime served each tool call.
- Add a discovery/capabilities service to detect valid Docker, Podman and embedded runtime sockets/binaries/configuration.

Exit Criteria:
- No visible change for users when runtime=host.
- Tools compile against new interfaces.
- The system can report which runtimes are installed or available.

### Phase 2 — Docker/Podman Support for Shell Commands
**fact_id:** 5

Objective: run `bash` inside Docker and Podman containers with session persistence.

Deliverables:
- Docker adapter with the engine's Go SDK.
- Podman adapter with Go bindings/REST socket.
- Image management: check local availability, resolve Docker/OCI images and optional pull based on policy.
- Persistent container management per session/project to emulate the current shell.
- Timeout, cancellation, stdout/stderr retrieval and cleanup.

Technical recommendation:
- Docker: use official/compatible Go SDK (`client`, `ContainerCreate`, `ContainerStart`, `ContainerExecCreate/Attach` or equivalent flow, `ImagePull`, `CopyToContainer`/`CopyFromContainer` when needed).
- Podman: use Go bindings (`go.podman.io/podman/.../bindings`, `images.Pull`, `containers.CreateWithSpec`, `containers.Start`, `containers.Exec...`) connecting to the rootless/rootful socket.
- Prefer a configurable, minimal base image with explicit shell; do not assume `/bin/bash` always.

Exit Criteria:
- `bash` supports host, docker and podman without changing the tool's external API.
- CWD and shell state persistence is resolved per session.
- Permissions show the requested runtime and image.

### Phase 3 — Containerized Support for view/write/edit/patch
**fact_id:** 6

Objective: make file operations work within the same containerized workspace.

Recommended strategy:
- **First iteration**: bind-mount of the host workspace in the container. This way `view/write/edit/patch` can continue operating on host paths while `bash` runs isolated.
- **Optional second iteration**: introduce real `WorkspaceFS` for operations purely inside the container using copy/archive APIs.

Deliverables:
- Host-backed and container-backed `WorkspaceFS`.
- Refactoring of `view.go`, `write.go`, `edit.go`, `patch.go` to use `WorkspaceFS` instead of direct `os.*` calls where appropriate.
- Alignment of `recordFileRead`/`recordFileWrite`, `withFileLock`, history and modtimes.
- Definition of what "read before write" means when the runtime is not direct host.
- Consistent support for base images and workspace↔container synchronization when a non-bind-mounted mode is activated.

Exit Criteria:
- `view/write/edit/patch` still respect security, locking and history.
- The workspace visible to shell and file tools is consistent.

### Phase 4 — Cross-cutting Integration, Security and UX
**fact_id:** 7

Objective: make the functionality operable and secure in CLI/TUI/API/Web UI.

Deliverables:
- Configuration exposed in API, TUI, Web UI/settings and persistent configuration file.
- Runtime and image selector with visual auto-detection of Docker, Podman and embedded runtime availability.
- Security policies per runtime and image.
- Container lifecycle logging/observability.
- Unit/e2e tests equivalent to ACP security tests.
- Usage documentation, fallback behavior and automatic/manual selection priorities.

Minimum recommended policies:
- rootless when the runtime allows it
- `network=none` by default
- configurable CPU/mem/pids limits
- read-only root filesystem except for necessary mounts
- non-root user inside the container
- allowlist for mounts and environment variables
- do not propagate host credentials by default

Exit Criteria:
- User can choose runtime per project/session/global config from TUI, Web UI/API and configuration file.
- The system shows whether Docker/Podman are installed, configurable or not available.
- Socket, pull or missing image failures produce clear errors.
- Acceptable security and regression coverage.

### Phase 5 — Custom Embedded Runtime and Registry Download
**fact_id:** 8

Objective: add a custom backend decoupled from Docker/Podman to run and manage OCI/Docker images downloaded from registry.

Recommended MVP scope:
- Resolve image references and pull from registry.
- Local cache of blobs/manifests/layers with basic GC.
- Digest and image metadata verification.
- Rootfs unpack and workspace mounting.
- Isolated exec with namespaces/cgroups if the environment allows, with controlled fallback if not.
- Embedded runtime integration as a first-class option in config, TUI and Web UI, alongside Docker and Podman.

Libraries/standards to evaluate:
- `go-containerregistry` for OCI/Docker image pull/parsing.
- image-spec / distribution-spec / OCI layout for local store.
- `containerd`/`nerdctl`-style primitives if deciding not to implement too low-level from scratch.
- Only build complete custom isolation if the project scope justifies it; otherwise, an "embedded image manager + delegated executor" may be sufficient.

Specific risks:
- High complexity in secure isolation, mounts, cgroups and Linux compatibility.
- Host privileges/capabilities requirement.
- Significant maintenance compared to reusing Docker/Podman/containerd.

Exit Criteria:
- Embedded runtime reuses the same `ContainerRuntime` contract.
- Pull from registry, Docker/OCI image usage and local cache work without breaking Docker/Podman.
- Clean fallback to external runtimes exists.

## Recommended Implementation Order
1. Contracts + discovery + config (`host` default)
2. Bash on Docker/Podman
3. WorkspaceFS and file tools
4. Security/UX/config surfaces
5. Embedded runtime + registry

## Specific Go Library Recommendations
- **Docker**: Docker/Moby engine client for `ImagePull`, `ContainerCreate/Start`, `Exec`, logs, copy/archive.
- **Podman**: official Podman Go bindings over REST socket.
- **Registry/OCI for phase 5**: `go-containerregistry` as the base for pull, manifests, layers and auth; evaluate complementing it with OCI/containerd primitives instead of building an entire runtime from scratch on day one.

## Final Note
The best technical path is to treat Docker and Podman as the first backends of a common contract, share the same configuration model across file, TUI and Web UI/API, and use ACP as the security/capabilities reference. The embedded runtime should be a later phase, first focused on image distribution/cache and only then on complete isolation, so as not to block the immediate value of container support.
