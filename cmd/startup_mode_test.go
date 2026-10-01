package cmd

import "testing"

func TestResolveStartupContextPublicBasePath(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "parent-1")
	t.Setenv("PANDO_PROJECT_ID", "project-1")
	t.Setenv("PANDO_PUBLIC_BASE", "/api/v1/projects/project-1/web")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.Mode != "project-child" {
		t.Fatalf("mode = %q, want project-child", ctx.Mode)
	}
	if ctx.PublicBasePath != "/api/v1/projects/project-1/web" {
		t.Fatalf("public base path = %q, want %q", ctx.PublicBasePath, "/api/v1/projects/project-1/web")
	}
}

func TestResolveStartupContextIgnoresInvalidPublicBasePath(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "parent-1")
	t.Setenv("PANDO_PROJECT_ID", "project-1")
	t.Setenv("PANDO_PUBLIC_BASE", "/not/a/project/web")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.PublicBasePath != "" {
		t.Fatalf("public base path = %q, want empty", ctx.PublicBasePath)
	}
}

func TestResolveStartupContextIgnoresPublicBasePathOutsideChildMode(t *testing.T) {
	t.Setenv("PANDO_PARENT_INSTANCE", "")
	t.Setenv("PANDO_PROJECT_ID", "")
	t.Setenv("PANDO_PUBLIC_BASE", "/api/v1/projects/project-1/web")

	ctx := resolveStartupContext(t.TempDir(), "serve")
	if ctx.Mode != "serve" {
		t.Fatalf("mode = %q, want serve", ctx.Mode)
	}
	if ctx.PublicBasePath != "" {
		t.Fatalf("public base path = %q, want empty", ctx.PublicBasePath)
	}
}
