package sandbox

import (
	"path"
	"strings"
)

// ScrubEnv returns the environment a sandboxed child should receive, given
// the parent's env in os.Environ() form ("NAME=value"). It never modifies the
// input. For a disabled policy it returns an unchanged copy.
//
// Decision order for each variable (names compared case-insensitively):
//
//  1. Env.Exclude match: dropped.
//  2. Env.Keep match: kept.
//  3. Always dropped: DBUS_SESSION_BUS_ADDRESS (a desktop-bus escape hatch),
//     and SSH_AUTH_SOCK when the network is restricted.
//  4. Credential-looking (when Env.ScrubSecrets): dropped. See IsSecretEnvName.
//  5. Env.Inherit: "all" keeps the rest, "core" keeps only IsCoreEnvName,
//     "none" drops the rest.
//
// Entries without a name (Windows "=C:=C:\\" drive entries) are kept.
func ScrubEnv(env []string, p Policy) []string {
	out := make([]string, 0, len(env))
	if !p.Enabled() {
		return append(out, env...)
	}
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == "" {
			out = append(out, kv)
			continue
		}
		upper := strings.ToUpper(name)
		switch {
		case matchAnyEnvPattern(p.Env.Exclude, upper):
			continue
		case matchAnyEnvPattern(p.Env.Keep, upper):
			out = append(out, kv)
			continue
		case upper == "DBUS_SESSION_BUS_ADDRESS":
			continue
		case upper == "SSH_AUTH_SOCK" && p.RestrictsNetwork():
			continue
		case p.Env.ScrubSecrets && IsSecretEnvName(upper):
			continue
		}
		switch p.Env.Inherit {
		case EnvInheritNone:
			continue
		case EnvInheritCore:
			if !IsCoreEnvName(upper) {
				continue
			}
		}
		out = append(out, kv)
	}
	return out
}

// secretEnvSubstrings mark a variable name as credential-looking wherever
// they appear.
var secretEnvSubstrings = []string{
	"API_KEY", "APIKEY", "ACCESS_KEY", "PRIVATE_KEY", "SECRET", "TOKEN",
	"PASSWORD", "PASSWD", "CREDENTIAL",
}

// secretEnvSuffixes mark a name as credential-looking at its end.
var secretEnvSuffixes = []string{"_KEY", "_PAT", "_PASS", "_DSN"}

// notSecretEnvNames are false positives of the patterns above.
var notSecretEnvNames = map[string]bool{
	"TOKENIZERS_PARALLELISM": true,
}

// IsSecretEnvName reports whether a variable name looks like it holds a
// credential: provider keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, ...), tokens
// (GITHUB_TOKEN, GH_TOKEN, NPM_TOKEN, AWS_SESSION_TOKEN, ...), secrets and
// passwords. Core variables (IsCoreEnvName) are never secret.
func IsSecretEnvName(name string) bool {
	upper := strings.ToUpper(name)
	if notSecretEnvNames[upper] || IsCoreEnvName(upper) {
		return false
	}
	for _, s := range secretEnvSubstrings {
		if strings.Contains(upper, s) {
			return true
		}
	}
	for _, s := range secretEnvSuffixes {
		if strings.HasSuffix(upper, s) {
			return true
		}
	}
	return false
}

// coreEnvNames is the "core" inherit set: what a shell, the common toolchains
// and terminal programs need to work.
var coreEnvNames = map[string]bool{
	// POSIX / shell / terminal.
	"PATH": true, "HOME": true, "USER": true, "USERNAME": true, "LOGNAME": true, "SHELL": true,
	"TERM": true, "TERM_PROGRAM": true, "TERM_PROGRAM_VERSION": true, "COLORTERM": true,
	"COLUMNS": true, "LINES": true, "LANG": true, "LANGUAGE": true, "TZ": true,
	"TMPDIR": true, "TMP": true, "TEMP": true, "PWD": true, "OLDPWD": true, "HOSTNAME": true,
	"EDITOR": true, "VISUAL": true, "PAGER": true, "LESS": true, "NO_COLOR": true, "FORCE_COLOR": true,
	"CI": true, "SSH_AUTH_SOCK": true, "DISPLAY": true, "WAYLAND_DISPLAY": true, "XAUTHORITY": true,
	"XDG_CONFIG_HOME": true, "XDG_CACHE_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	"XDG_RUNTIME_DIR": true,
	"HTTP_PROXY":      true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
	// Toolchains.
	"GOPATH": true, "GOROOT": true, "GOCACHE": true, "GOMODCACHE": true, "GOFLAGS": true,
	"GOPROXY": true, "GOPRIVATE": true, "GONOSUMDB": true, "GONOPROXY": true, "GOTOOLCHAIN": true,
	"CGO_ENABLED": true, "CARGO_HOME": true, "RUSTUP_HOME": true, "NVM_DIR": true, "NODE_PATH": true,
	"PYENV_ROOT": true, "VIRTUAL_ENV": true, "JAVA_HOME": true,
	// Windows.
	"SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true, "USERPROFILE": true,
	"APPDATA": true, "LOCALAPPDATA": true, "PROGRAMDATA": true, "PROGRAMFILES": true,
	"PROGRAMFILES(X86)": true, "PROGRAMW6432": true, "COMMONPROGRAMFILES": true, "SYSTEMDRIVE": true,
	"HOMEDRIVE": true, "HOMEPATH": true, "NUMBER_OF_PROCESSORS": true, "PROCESSOR_ARCHITECTURE": true,
	"OS": true, "PSMODULEPATH": true,
}

// IsCoreEnvName reports whether name is in the "core" inherit set (PATH,
// HOME, TERM, LANG, LC_*, SSH_AUTH_SOCK, toolchain roots, Windows system
// variables, ...).
func IsCoreEnvName(name string) bool {
	upper := strings.ToUpper(name)
	return coreEnvNames[upper] || strings.HasPrefix(upper, "LC_")
}

// matchAnyEnvPattern matches an upper-cased name against glob patterns
// ('*', '?', '[...]'), case-insensitively. A malformed pattern matches only
// the identical name.
func matchAnyEnvPattern(patterns []string, upper string) bool {
	for _, pat := range patterns {
		pat = strings.ToUpper(strings.TrimSpace(pat))
		if pat == "" {
			continue
		}
		ok, err := path.Match(pat, upper)
		if err != nil {
			ok = pat == upper
		}
		if ok {
			return true
		}
	}
	return false
}
