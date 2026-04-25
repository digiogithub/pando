// Package runtime provides the execution and workspace abstraction used to run
// tool commands either on the host or inside an isolated container runtime.
//
// The package is consumed through RuntimeResolver implementations. Most callers
// use NewResolver() together with config.ContainerConfig:
//
//	resolver := runtime.NewResolver()
//	execRuntime, workspaceFS, err := resolver.Resolve(cfg.Container)
//
// Resolve returns both an ExecutionRuntime for bash-style command execution and
// a WorkspaceFS that keeps file tools pointed at the same workspace. Host mode
// preserves the historical behaviour, while Docker and Podman use bind-mounted
// workspaces so view/write/edit/patch operate on the same files that commands
// see inside the container.
//
// Container configuration lives under the [Container] section in .pando.toml.
// Supported keys are:
//   - runtime: host, docker, podman, embedded, or auto
//   - image, pull_policy, socket, work_dir
//   - network, read_only, user, cpu_limit, mem_limit, pids_limit
//   - no_new_privileges, allow_env, allow_mounts, extra_env, extra_mounts
//   - embedded_cache_dir, embedded_gc_keep_n
//
// The secure defaults assume an isolated runtime: network access disabled,
// read-only root filesystem enabled, no-new-privileges enabled, and a PID cap
// of 512. These defaults reduce accidental network exfiltration, make root
// filesystem mutation explicit, and limit the blast radius of runaway or
// hostile processes. The workspace bind mount remains writable so normal tool
// editing flows still work.
//
// When runtime is set to auto, the resolver prefers a rootless Podman socket,
// then Docker, and finally falls back to the host runtime. Manual selection is
// available for Docker, Podman, or the embedded runtime.
package runtime
