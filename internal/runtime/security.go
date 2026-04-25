package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/digiogithub/pando/internal/config"
)

type SecurityPolicy struct {
	Network         string
	ReadOnly        bool
	User            string
	CPULimit        string
	MemLimit        string
	PidsLimit       int64
	AllowEnv        []string
	AllowMounts     []string
	NoNewPrivileges bool
}

func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		Network:         "none",
		ReadOnly:        true,
		NoNewPrivileges: true,
		PidsLimit:       512,
	}
}

func PolicyFromConfig(cfg config.ContainerConfig) SecurityPolicy {
	policy := DefaultSecurityPolicy()

	if strings.TrimSpace(cfg.Network) != "" {
		policy.Network = strings.TrimSpace(cfg.Network)
	}
	if cfg.ReadOnly {
		policy.ReadOnly = true
	}
	if strings.TrimSpace(cfg.User) != "" {
		policy.User = strings.TrimSpace(cfg.User)
	}
	if strings.TrimSpace(cfg.CPULimit) != "" {
		policy.CPULimit = strings.TrimSpace(cfg.CPULimit)
	}
	if strings.TrimSpace(cfg.MemLimit) != "" {
		policy.MemLimit = strings.TrimSpace(cfg.MemLimit)
	}
	if cfg.PidsLimit > 0 {
		policy.PidsLimit = cfg.PidsLimit
	}
	if len(cfg.AllowEnv) > 0 {
		policy.AllowEnv = append([]string(nil), cfg.AllowEnv...)
	}
	if len(cfg.AllowMounts) > 0 {
		policy.AllowMounts = append([]string(nil), cfg.AllowMounts...)
	}
	if cfg.NoNewPrivileges {
		policy.NoNewPrivileges = true
	}

	return policy
}

func applySecurityPolicy(cfg config.ContainerConfig) (SecurityPolicy, []string, []string, error) {
	policy := PolicyFromConfig(cfg)

	env, err := filterAllowedEnv(cfg.ExtraEnv, policy.AllowEnv)
	if err != nil {
		return SecurityPolicy{}, nil, nil, err
	}
	mounts, err := filterAllowedMounts(cfg.ExtraMounts, policy.AllowMounts)
	if err != nil {
		return SecurityPolicy{}, nil, nil, err
	}

	return policy, env, mounts, nil
}

func filterAllowedEnv(extraEnv []string, allowlist []string) ([]string, error) {
	if len(extraEnv) == 0 {
		return nil, nil
	}
	if len(allowlist) == 0 {
		return append([]string(nil), extraEnv...), nil
	}

	allowed := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}

	out := make([]string, 0, len(extraEnv))
	for _, item := range extraEnv {
		name, _, ok := strings.Cut(item, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid environment entry %q", item)
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("environment variable %q is not allowed by container policy", name)
		}
		out = append(out, item)
	}

	return out, nil
}

func filterAllowedMounts(extraMounts []string, allowlist []string) ([]string, error) {
	if len(extraMounts) == 0 {
		return nil, nil
	}
	if len(allowlist) == 0 {
		return append([]string(nil), extraMounts...), nil
	}

	allowed := make([]string, 0, len(allowlist))
	for _, path := range allowlist {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		allowed = append(allowed, filepath.Clean(path))
	}

	out := make([]string, 0, len(extraMounts))
	for _, mount := range extraMounts {
		source, _, ok := strings.Cut(mount, ":")
		source = strings.TrimSpace(source)
		if !ok || source == "" {
			return nil, fmt.Errorf("invalid mount entry %q", mount)
		}

		cleanSource := filepath.Clean(source)
		permitted := false
		for _, allowedPath := range allowed {
			if cleanSource == allowedPath || strings.HasPrefix(cleanSource, allowedPath+string(os.PathSeparator)) {
				permitted = true
				break
			}
		}
		if !permitted {
			return nil, fmt.Errorf("mount path %q is not allowed by container policy", source)
		}
		out = append(out, mount)
	}

	return out, nil
}
