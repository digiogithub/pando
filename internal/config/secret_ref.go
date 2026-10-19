package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/digiogithub/pando/internal/llm/models"
	"github.com/digiogithub/pando/internal/logging"
)

// secretRefPrefix marks a configuration value as a reference to a process
// environment variable instead of a literal secret.
const secretRefPrefix = "env:"

// secretRefPattern matches a complete "env:NAME" reference where NAME is a
// conventional environment variable name.
var secretRefPattern = regexp.MustCompile(`^env:([A-Za-z_][A-Za-z0-9_]*)$`)

// warnedUnsetRefs remembers the variable names already reported as unset so
// each one is logged once per process.
var warnedUnsetRefs sync.Map

// IsSecretRef reports whether v is an "env:NAME" secret reference.
func IsSecretRef(v string) bool {
	return secretRefPattern.MatchString(strings.TrimSpace(v))
}

// ResolveSecretRef returns the value v stands for. A literal is returned
// unchanged. An "env:NAME" reference is replaced by the current value of the
// environment variable NAME, evaluated at call time so a process that is
// restarted with a different environment, or whose environment changes, sees
// the new value. An unset variable resolves to the empty string and is
// reported once through the log, naming the variable but never a value.
func ResolveSecretRef(v string) string {
	m := secretRefPattern.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return v
	}
	val, ok := os.LookupEnv(m[1])
	if !ok || val == "" {
		if _, seen := warnedUnsetRefs.LoadOrStore(m[1], true); !seen {
			logging.Warn("secret reference points to an unset environment variable", "variable", m[1])
		}
		return ""
	}
	return val
}

// Resolved returns a copy of the account whose APIKey and ExtraHeaders values
// have their "env:NAME" references replaced by the environment values. The
// receiver is not modified and the copy must never be written back to the
// configuration file: use it only to talk to the upstream provider.
func (a ProviderAccount) Resolved() ProviderAccount {
	out := a
	out.APIKey = ResolveSecretRef(a.APIKey)
	if len(a.ExtraHeaders) > 0 {
		out.ExtraHeaders = make(map[string]string, len(a.ExtraHeaders))
		for k, v := range a.ExtraHeaders {
			out.ExtraHeaders[k] = ResolveSecretRef(v)
		}
	}
	return out
}

// GetResolvedProviderAccounts returns the configured provider accounts with
// secret references resolved from the environment. Use it where the account is
// used to reach a provider; use GetProviderAccounts where it is displayed or
// edited so that references, not secrets, are what leaves the process.
func GetResolvedProviderAccounts() []ProviderAccount {
	accounts := GetProviderAccounts()
	for i := range accounts {
		accounts[i] = accounts[i].Resolved()
	}
	return accounts
}

// mergeEnvProvidersIntoAccounts adds an account (ID equal to the provider type)
// for every legacy or environment-backed provider that has credentials and
// whose type has no account yet. It lets a configuration declare a few
// [[providerAccounts]] without losing providers whose keys only exist in the
// process environment. Types that already have an account are left alone. The
// result lives in memory only: the configuration file is never rewritten from it.
func mergeEnvProvidersIntoAccounts(c *Config) {
	if len(c.ProviderAccounts) == 0 || len(c.Providers) == 0 {
		return
	}
	haveType := make(map[string]bool, len(c.ProviderAccounts))
	haveID := make(map[string]bool, len(c.ProviderAccounts))
	for _, acc := range c.ProviderAccounts {
		haveType[string(acc.Type)] = true
		haveID[acc.ID] = true
	}
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, string(name))
	}
	sort.Strings(names)
	for _, name := range names {
		if haveType[name] || haveID[name] {
			continue
		}
		p := c.Providers[models.ModelProvider(name)]
		if !providerEntryHasCredentials(models.ModelProvider(name), p) {
			continue
		}
		c.ProviderAccounts = append(c.ProviderAccounts, ProviderAccount{
			ID:          name,
			DisplayName: capitalizeFirst(name),
			Type:        models.ModelProvider(name),
			APIKey:      p.APIKey,
			BaseURL:     p.BaseURL,
			Disabled:    p.Disabled,
		})
		logging.Debug("kept environment provider beside provider accounts", "provider", name)
	}
}
