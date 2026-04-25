package runtime

import (
	"os"
	"os/exec"
	"strings"
)

// Discover probes the host for available container runtimes and returns their
// capabilities. The host runtime is always reported as available.
func Discover() []RuntimeCapabilities {
	return []RuntimeCapabilities{
		{
			Type:      RuntimeHost,
			Available: true,
			Exec:      true,
			FS:        true,
		},
		{
			Type:      RuntimeEmbedded,
			Available: true,
			Exec:      true,
			FS:        false,
		},
		discoverDocker(),
		discoverPodman(),
	}
}

func discoverDocker() RuntimeCapabilities {
	c := RuntimeCapabilities{Type: RuntimeDocker}

	path, err := exec.LookPath("docker")
	if err != nil {
		return c
	}

	// Verify the binary is executable by asking for the client version.
	out, err := exec.Command(path, "version", "--format", "{{.Client.Version}}").Output()
	if err == nil {
		c.Version = strings.TrimSpace(string(out))
	}

	// Resolve the socket path.
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		c.Socket = host
	} else if fileExists("/var/run/docker.sock") {
		c.Socket = "/var/run/docker.sock"
	}

	c.Available = c.Socket != ""
	if c.Available {
		c.Exec = true
		c.FS = true
	}
	return c
}

func discoverPodman() RuntimeCapabilities {
	c := RuntimeCapabilities{Type: RuntimePodman}

	path, err := exec.LookPath("podman")
	if err != nil {
		return c
	}

	out, err := exec.Command(path, "version", "--format", "{{.Client.Version}}").Output()
	if err == nil {
		c.Version = strings.TrimSpace(string(out))
	}

	// Resolve the socket path — prefer XDG_RUNTIME_DIR, then /run/podman.
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidate := xdg + "/podman/podman.sock"
		if fileExists(candidate) {
			c.Socket = candidate
		}
	}
	if c.Socket == "" && fileExists("/run/podman/podman.sock") {
		c.Socket = "/run/podman/podman.sock"
	}

	c.Available = c.Socket != ""
	if c.Available {
		c.Exec = true
		c.FS = true
	}
	return c
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
