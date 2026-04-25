package runtime

import (
	"fmt"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

// defaultResolver is the package-level RuntimeResolver implementation.
type defaultResolver struct{}

// NewResolver returns the default RuntimeResolver.
func NewResolver() RuntimeResolver {
	return &defaultResolver{}
}

func (r *defaultResolver) Discover() []RuntimeCapabilities {
	return Discover()
}

// Resolve returns the ExecutionRuntime and WorkspaceFS for the given ContainerConfig.
// When runtime is "host" or empty the host implementations are returned, preserving
// existing behaviour without any behavioural change.
// When runtime is "auto", the best available runtime is chosen via Discover().
func (r *defaultResolver) Resolve(cfg config.ContainerConfig) (ExecutionRuntime, WorkspaceFS, error) {
	rt := cfg.Runtime
	if rt == "" {
		rt = string(RuntimeHost)
	}

	switch RuntimeType(rt) {
	case RuntimeHost:
		return NewHostRuntime(), NewHostFS(), nil

	case RuntimeDocker:
		runtime, err := NewDockerRuntime(cfg)
		if err != nil {
			return nil, nil, err
		}
		return runtime, NewContainerFS(), nil

	case RuntimePodman:
		runtime, err := NewPodmanRuntime(cfg)
		if err != nil {
			return nil, nil, err
		}
		return runtime, NewContainerFS(), nil

	case RuntimeEmbedded:
		runtime, err := NewEmbeddedRuntime(cfg)
		if err != nil {
			return nil, nil, err
		}
		return runtime, NewHostFS(), nil

	case "auto":
		return r.resolveAuto(cfg)

	default:
		return nil, nil, fmt.Errorf("unknown runtime %q", rt)
	}
}

// resolveAuto picks the best available runtime discovered on the host.
// It prefers rootless Podman, then Docker, and finally falls back to host.
func (r *defaultResolver) resolveAuto(cfg config.ContainerConfig) (ExecutionRuntime, WorkspaceFS, error) {
	caps := r.Discover()
	var dockerCap *RuntimeCapabilities
	var rootlessPodmanCap *RuntimeCapabilities

	for i := range caps {
		cap := &caps[i]
		if !cap.Available {
			continue
		}
		switch cap.Type {
		case RuntimeDocker:
			dockerCap = cap
		case RuntimePodman:
			if isRootlessPodmanSocket(cap.Socket) {
				rootlessPodmanCap = cap
			}
		}
	}

	if rootlessPodmanCap != nil {
		cfg.Runtime = string(RuntimePodman)
		if runtime, err := NewPodmanRuntime(cfg); err == nil {
			return runtime, NewContainerFS(), nil
		}
	}

	if dockerCap != nil {
		cfg.Runtime = string(RuntimeDocker)
		if runtime, err := NewDockerRuntime(cfg); err == nil {
			return runtime, NewContainerFS(), nil
		}
	}

	return NewHostRuntime(), NewHostFS(), nil
}

func isRootlessPodmanSocket(socket string) bool {
	socket = strings.TrimSpace(socket)
	return socket != "" && strings.Contains(socket, "/user/")
}
