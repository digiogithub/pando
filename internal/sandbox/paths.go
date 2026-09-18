package sandbox

import (
	"path/filepath"
	"strings"
)

// pathEnv is the environment/home view the path tables are computed from.
// It is a parameter (not os.Getenv) so tests can resolve policies for any OS.
type pathEnv struct {
	goos   string
	home   string
	lookup func(string) (string, bool)
}

// get returns an environment value, "" when unset.
func (e pathEnv) get(key string) string {
	if e.lookup == nil {
		return ""
	}
	v, _ := e.lookup(key)
	return strings.TrimSpace(v)
}

// absEnv returns an environment value only when it is an absolute path.
func (e pathEnv) absEnv(key string) string {
	v := e.get(key)
	if v == "" || !isAbs(v, e.goos) {
		return ""
	}
	return filepath.Clean(v)
}

func (e pathEnv) inHome(elem ...string) string {
	if e.home == "" {
		return ""
	}
	return filepath.Join(append([]string{e.home}, elem...)...)
}

// isAbs is filepath.IsAbs, also accepting drive paths when resolving a
// Windows policy on another OS (tests) and vice versa.
func isAbs(p, goos string) bool {
	if filepath.IsAbs(p) {
		return true
	}
	if goos == "windows" {
		return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
	}
	return strings.HasPrefix(p, "/")
}

// tempRoots returns the temporary directories a sandboxed child may write,
// for the given OS (following Grok Build's paths.rs temp_writable_paths).
func tempRoots(e pathEnv) []string {
	var roots []string
	switch e.goos {
	case "windows":
		roots = append(roots, e.absEnv("TEMP"), e.absEnv("TMP"))
		if lad := e.absEnv("LOCALAPPDATA"); lad != "" {
			roots = append(roots, filepath.Join(lad, "Temp"))
		}
	default:
		roots = append(roots, "/tmp", "/var/tmp")
		if e.goos == "darwin" {
			// /tmp and /var are symlinks into /private, and the real
			// per-user TMPDIR lives under /private/var/folders.
			roots = append(roots, "/private/tmp", "/private/var/tmp", "/private/var/folders")
		}
		roots = append(roots, e.absEnv("TMPDIR"))
	}
	return roots
}

// userCacheDir mirrors os.UserCacheDir for the given OS.
func userCacheDir(e pathEnv) string {
	switch e.goos {
	case "windows":
		return e.absEnv("LOCALAPPDATA")
	case "darwin":
		return e.inHome("Library", "Caches")
	default:
		if xdg := e.absEnv("XDG_CACHE_HOME"); xdg != "" {
			return xdg
		}
		return e.inHome(".cache")
	}
}

// cacheRoots returns the dependency/build cache directories kept writable in
// workspace-write mode so package managers and builds work: Go build and
// module caches, npm, pnpm, yarn, pip, cargo, bun, gradle, maven, and the
// user cache dir (~/.cache, ~/Library/Caches). Tool-specific env overrides
// (GOCACHE, GOMODCACHE, npm_config_cache, ...) are honoured when absolute.
func cacheRoots(e pathEnv) []string {
	var roots []string
	add := func(p ...string) {
		for _, v := range p {
			if v != "" {
				roots = append(roots, v)
			}
		}
	}

	ucd := userCacheDir(e)
	if e.goos == "windows" {
		// %LOCALAPPDATA% holds far more than caches; list the cache subdirs.
		if ucd != "" {
			add(filepath.Join(ucd, "go-build"),
				filepath.Join(ucd, "npm-cache"),
				filepath.Join(ucd, "pnpm"),
				filepath.Join(ucd, "pnpm-cache"),
				filepath.Join(ucd, "Yarn", "Cache"),
				filepath.Join(ucd, "pip", "Cache"))
		}
	} else {
		add(ucd)
	}

	// Go.
	add(e.absEnv("GOCACHE"), e.absEnv("GOMODCACHE"))
	if gopath := e.get("GOPATH"); gopath != "" {
		sep := ":"
		if e.goos == "windows" {
			sep = ";"
		}
		first := strings.Split(gopath, sep)[0]
		if isAbs(first, e.goos) {
			add(filepath.Join(filepath.Clean(first), "pkg", "mod"))
		}
	} else {
		add(e.inHome("go", "pkg", "mod"))
	}

	// JavaScript.
	add(e.absEnv("npm_config_cache"), e.absEnv("NPM_CONFIG_CACHE"), e.absEnv("YARN_CACHE_FOLDER"),
		e.absEnv("BUN_INSTALL_CACHE_DIR"))
	if e.goos != "windows" {
		add(e.inHome(".npm"), e.inHome(".pnpm-store"), e.inHome(".yarn", "berry"))
	}
	switch e.goos {
	case "darwin":
		add(e.inHome("Library", "pnpm", "store"))
	case "windows":
	default:
		add(e.inHome(".local", "share", "pnpm", "store"))
	}
	add(e.inHome(".bun", "install", "cache"))

	// Python.
	add(e.absEnv("PIP_CACHE_DIR"))

	// Rust: only the download caches, never the toolchain or bin dirs.
	cargoHome := e.absEnv("CARGO_HOME")
	if cargoHome == "" {
		cargoHome = e.inHome(".cargo")
	}
	if cargoHome != "" {
		add(filepath.Join(cargoHome, "registry"), filepath.Join(cargoHome, "git"))
	}

	// JVM.
	add(e.inHome(".gradle", "caches"), e.inHome(".m2", "repository"))
	return roots
}

// systemReadRoots returns the directories strict mode keeps readable besides
// the workspace and writable roots: system libraries, config and toolchains.
func systemReadRoots(e pathEnv) []string {
	var roots []string
	switch e.goos {
	case "windows":
		for _, key := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramData"} {
			roots = append(roots, e.absEnv(key))
		}
	case "darwin":
		roots = append(roots, "/usr", "/bin", "/sbin", "/etc", "/private", "/var", "/dev",
			"/System", "/Library", "/Applications", "/opt", "/nix")
		roots = append(roots, e.inHome("Library"))
	default:
		roots = append(roots, "/usr", "/bin", "/sbin", "/lib", "/lib32", "/lib64", "/libx32", "/etc",
			"/run", "/var", "/opt", "/proc", "/sys", "/dev", "/nix", "/snap")
	}
	// Toolchains and their caches under $HOME.
	roots = append(roots, e.absEnv("GOROOT"),
		e.inHome("go"), e.inHome("sdk"), e.inHome(".cargo"), e.inHome(".rustup"),
		e.inHome(".nvm"), e.inHome(".bun"), e.inHome(".local"), e.inHome(".npm"),
		e.inHome(".pyenv"), e.inHome(".asdf"), e.inHome(".gitconfig"), e.inHome(".config", "git"))
	roots = append(roots, userCacheDir(e))
	return roots
}

// globalConfigDirs mirrors config.GlobalConfigDir plus the legacy search
// locations: $XDG_CONFIG_HOME/pando and ~/.config/pando.
func globalConfigDirs(e pathEnv) []string {
	var dirs []string
	if xdg := e.absEnv("XDG_CONFIG_HOME"); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "pando"))
	}
	dirs = append(dirs, e.inHome(".config", "pando"))
	return dirs
}

// protectedPaths returns the paths kept read-only even inside writable roots:
// Pando's project and global config and data (so the agent cannot switch its
// own sandbox off) and the git code-exec vectors. The rest of .git stays
// writable so git commit works.
func protectedPaths(e pathEnv, workspace, dataDir, localConfigFile string) []string {
	var out []string
	if workspace != "" {
		out = append(out,
			filepath.Join(workspace, ".pando"),
			filepath.Join(workspace, ".pando.toml"),
			filepath.Join(workspace, ".pando.json"),
			filepath.Join(workspace, ".git", "hooks"),
			filepath.Join(workspace, ".git", "config"),
		)
	}
	if localConfigFile != "" {
		out = append(out, absUnder(workspace, localConfigFile, e.goos))
	}
	if dataDir != "" {
		out = append(out, absUnder(workspace, dataDir, e.goos))
	}
	for _, ext := range []string{"toml", "json", "yaml", "yml"} {
		if p := e.inHome(".pando." + ext); p != "" {
			out = append(out, p)
		}
	}
	out = append(out, globalConfigDirs(e)...)
	return out
}

// absUnder makes p absolute, resolving a relative p against base.
func absUnder(base, p, goos string) string {
	if p == "" {
		return ""
	}
	if !isAbs(p, goos) && base != "" {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// expandPath expands a leading "~", then resolves a relative path against
// base. Returns "" for an empty input.
func expandPath(p, home, base, goos string) string {
	p = strings.TrimSpace(p)
	switch {
	case p == "":
		return ""
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`):
		if home == "" {
			return ""
		}
		p = filepath.Join(home, p[2:])
	}
	return absUnder(base, p, goos)
}

// cleanList drops empty entries and sorts/deduplicates the rest.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return sortedCopy(out)
}
