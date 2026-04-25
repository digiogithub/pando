package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/config"
	"github.com/docker/go-units"
)

const (
	defaultContainerPullPolicy = "if-not-present"
	defaultContainerNetwork    = "none"
)

var containerNameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

type containerSession struct {
	ContainerID string
	WorkDir     string

	mu     sync.Mutex
	output string
}

func (s *containerSession) setOutput(stdout, stderr string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case stdout != "" && stderr != "":
		s.output = stdout + "\n" + stderr
	case stdout != "":
		s.output = stdout
	default:
		s.output = stderr
	}
}

func (s *containerSession) getOutput() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output
}

func normalizeContainerConfig(cfg config.ContainerConfig) config.ContainerConfig {
	if strings.TrimSpace(cfg.PullPolicy) == "" {
		cfg.PullPolicy = defaultContainerPullPolicy
	}
	if strings.TrimSpace(cfg.Network) == "" {
		cfg.Network = defaultContainerNetwork
	}
	return cfg
}

func containerWorkDir(cfg config.ContainerConfig, hostWorkDir string) string {
	if strings.TrimSpace(cfg.WorkDir) != "" {
		return cfg.WorkDir
	}
	return hostWorkDir
}

func sanitizeContainerName(prefix, sessionID string) string {
	safe := containerNameSanitizer.ReplaceAllString(sessionID, "-")
	safe = strings.Trim(safe, "-")
	if safe == "" {
		safe = "session"
	}
	return fmt.Sprintf("pando-%s-%s", prefix, safe)
}

func parseNanoCPUs(limit string) (int64, error) {
	if strings.TrimSpace(limit) == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(limit, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cpu limit %q: %w", limit, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid cpu limit %q: must be non-negative", limit)
	}
	return int64(value * 1_000_000_000), nil
}

func parseMemoryLimit(limit string) (int64, error) {
	if strings.TrimSpace(limit) == "" {
		return 0, nil
	}
	value, err := units.RAMInBytes(limit)
	if err != nil {
		return 0, fmt.Errorf("invalid memory limit %q: %w", limit, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid memory limit %q: must be non-negative", limit)
	}
	return value, nil
}

func matchesImageReference(imageRef, repoTag string) bool {
	if repoTag == imageRef {
		return true
	}
	if strings.Contains(imageRef, "@") {
		return false
	}
	if !strings.Contains(imageRef, ":") && repoTag == imageRef+":latest" {
		return true
	}
	return false
}
