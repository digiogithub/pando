package sandbox

import (
	"slices"
	"testing"
)

func TestIsSecretEnvName(t *testing.T) {
	secrets := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "PANDO_BRAVE_API_KEY", "GITHUB_TOKEN",
		"GH_TOKEN", "NPM_TOKEN", "AWS_SECRET_ACCESS_KEY", "AWS_ACCESS_KEY_ID", "AWS_SESSION_TOKEN",
		"DB_PASSWORD", "MYSQL_PWD_PASSWD", "GOOGLE_APPLICATION_CREDENTIALS", "STRIPE_SECRET",
		"deploy_key", "GITLAB_PAT", "SENTRY_DSN", "SSH_PRIVATE_KEY", "openrouter_apikey",
	}
	for _, name := range secrets {
		if !IsSecretEnvName(name) {
			t.Errorf("IsSecretEnvName(%q) = false, want true", name)
		}
	}
	benign := []string{
		"PATH", "HOME", "TERM", "LANG", "LC_ALL", "SSH_AUTH_SOCK", "GOPATH", "EDITOR", "SHELL",
		"TOKENIZERS_PARALLELISM", "KEYTIMEOUT", "GIT_ASKPASS", "XDG_RUNTIME_DIR", "NODE_ENV",
	}
	for _, name := range benign {
		if IsSecretEnvName(name) {
			t.Errorf("IsSecretEnvName(%q) = true, want false", name)
		}
	}
}

func TestScrubEnv(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"HOME=/home/dev",
		"LANG=C.UTF-8",
		"LC_ALL=C",
		"TERM=xterm",
		"SSH_AUTH_SOCK=/run/ssh.sock",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/bus",
		"ANTHROPIC_API_KEY=sk-ant",
		"GITHUB_TOKEN=ghp",
		"NODE_ENV=development",
		"MY_SERVICE_URL=http://x",
		"weird=has=equals",
		"=C:=C:\\",
	}
	enabled := Policy{Mode: ModeWorkspaceWrite, Network: NetworkAllowed, Env: EnvPolicy{Inherit: EnvInheritAll, ScrubSecrets: true}}

	t.Run("default drops secrets and dbus", func(t *testing.T) {
		got := ScrubEnv(env, enabled)
		want := []string{"PATH=/usr/bin", "HOME=/home/dev", "LANG=C.UTF-8", "LC_ALL=C", "TERM=xterm",
			"SSH_AUTH_SOCK=/run/ssh.sock", "NODE_ENV=development", "MY_SERVICE_URL=http://x",
			"weird=has=equals", "=C:=C:\\"}
		if !slices.Equal(got, want) {
			t.Fatalf("ScrubEnv = %v\nwant %v", got, want)
		}
	})

	t.Run("does not modify input", func(t *testing.T) {
		in := slices.Clone(env)
		_ = ScrubEnv(in, enabled)
		if !slices.Equal(in, env) {
			t.Fatal("input modified")
		}
	})

	t.Run("restricted network drops ssh agent", func(t *testing.T) {
		p := enabled
		p.Network = NetworkRestricted
		if slices.Contains(ScrubEnv(env, p), "SSH_AUTH_SOCK=/run/ssh.sock") {
			t.Fatal("SSH_AUTH_SOCK kept with a restricted network")
		}
	})

	t.Run("keep and exclude", func(t *testing.T) {
		p := enabled
		p.Env.Keep = []string{"github_*", "ssh_auth_sock"}
		p.Env.Exclude = []string{"MY_*", "SSH_AUTH_SOCK"}
		got := ScrubEnv(env, p)
		if !slices.Contains(got, "GITHUB_TOKEN=ghp") {
			t.Fatal("Keep did not keep GITHUB_TOKEN")
		}
		if slices.Contains(got, "MY_SERVICE_URL=http://x") || slices.Contains(got, "SSH_AUTH_SOCK=/run/ssh.sock") {
			t.Fatal("Exclude did not win")
		}
		if slices.Contains(got, "ANTHROPIC_API_KEY=sk-ant") {
			t.Fatal("unrelated secret kept")
		}
	})

	t.Run("keep secrets", func(t *testing.T) {
		p := enabled
		p.Env.ScrubSecrets = false
		if !slices.Contains(ScrubEnv(env, p), "ANTHROPIC_API_KEY=sk-ant") {
			t.Fatal("secret dropped although ScrubSecrets is false")
		}
	})

	t.Run("core", func(t *testing.T) {
		p := enabled
		p.Env.Inherit = EnvInheritCore
		got := ScrubEnv(env, p)
		want := []string{"PATH=/usr/bin", "HOME=/home/dev", "LANG=C.UTF-8", "LC_ALL=C", "TERM=xterm",
			"SSH_AUTH_SOCK=/run/ssh.sock", "=C:=C:\\"}
		if !slices.Equal(got, want) {
			t.Fatalf("ScrubEnv(core) = %v\nwant %v", got, want)
		}
	})

	t.Run("none keeps only Keep", func(t *testing.T) {
		p := enabled
		p.Env.Inherit = EnvInheritNone
		p.Env.Keep = []string{"PATH"}
		got := ScrubEnv(env, p)
		if !slices.Equal(got, []string{"PATH=/usr/bin", "=C:=C:\\"}) {
			t.Fatalf("ScrubEnv(none) = %v", got)
		}
	})

	t.Run("disabled policy passes everything", func(t *testing.T) {
		if got := ScrubEnv(env, Policy{Mode: ModeOff}); !slices.Equal(got, env) {
			t.Fatalf("ScrubEnv(off) = %v", got)
		}
	})
}
