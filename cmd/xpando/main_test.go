package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseModule(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantImport string
		wantModule string
		wantVer    string
		wantLocal  bool
		wantErr    bool
	}{
		{name: "bare", value: "example.com/mod", wantImport: "example.com/mod", wantModule: "example.com/mod"},
		{name: "subpackage", value: "example.com/mod/tools", wantImport: "example.com/mod/tools", wantModule: "example.com/mod/tools"},
		{name: "versioned", value: "example.com/mod/tools@v1.2.0", wantImport: "example.com/mod/tools", wantModule: "example.com/mod/tools", wantVer: "v1.2.0"},
		{name: "empty", value: "", wantErr: true},
		{name: "empty_version", value: "example.com/mod@", wantErr: true},
		{name: "empty_path", value: "@v1.0.0", wantErr: true},
		{name: "empty_local", value: "example.com/mod=", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseModule(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseModule(%q) = %+v, want error", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseModule(%q): %v", tt.value, err)
			}
			if got.importPath != tt.wantImport || got.modulePath != tt.wantModule || got.version != tt.wantVer {
				t.Fatalf("parseModule(%q) = %+v", tt.value, got)
			}
			if (got.localPath != "") != tt.wantLocal {
				t.Fatalf("parseModule(%q) localPath = %q", tt.value, got.localPath)
			}
		})
	}
}

// A local checkout is replaced by module root, not by the import path that was
// asked for: replacing "example.com/mod/tools" would not resolve anything.
func TestParseModuleLocalPathUsesModuleRoot(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "example.com/mod")

	got, err := parseModule("example.com/mod/tools=" + dir)
	if err != nil {
		t.Fatalf("parseModule: %v", err)
	}
	if got.importPath != "example.com/mod/tools" {
		t.Fatalf("importPath = %q", got.importPath)
	}
	if got.modulePath != "example.com/mod" {
		t.Fatalf("modulePath = %q, want the module root", got.modulePath)
	}
	if got.localPath != dir {
		t.Fatalf("localPath = %q", got.localPath)
	}
}

func TestParseModuleLocalPathRejectsForeignImport(t *testing.T) {
	dir := t.TempDir()
	writeGoMod(t, dir, "example.com/other")

	if _, err := parseModule("example.com/mod/tools=" + dir); err == nil {
		t.Fatal("expected an error when the import path is outside the local module")
	}
}

func TestParseModuleLocalPathRejectsVersion(t *testing.T) {
	if _, err := parseModule("example.com/mod@v1.0.0=/tmp/x"); err == nil {
		t.Fatal("expected an error for a version plus a local path")
	}
}

func TestModuleRootMissingGoMod(t *testing.T) {
	if _, err := moduleRoot(t.TempDir()); err == nil {
		t.Fatal("expected an error when the directory has no go.mod")
	}
}

func TestParseArgs(t *testing.T) {
	opts, err := parseArgs([]string{
		"v0.9.1",
		"--with", "example.com/mod/tools@v1.0.0",
		"--with=example.com/other",
		"--replace", "example.com/dep=example.com/fork@v2.0.0",
		"--tags", "enterprise,cuda",
		"--output", "./pando-enterprise",
		"--ldflags", "-X main.X=1",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if opts.coreVersion != "v0.9.1" {
		t.Fatalf("coreVersion = %q", opts.coreVersion)
	}
	if len(opts.with) != 2 || opts.with[0].version != "v1.0.0" || opts.with[1].importPath != "example.com/other" {
		t.Fatalf("with = %+v", opts.with)
	}
	if len(opts.replaces) != 1 || opts.replaces[0].to != "example.com/fork@v2.0.0" {
		t.Fatalf("replaces = %+v", opts.replaces)
	}
	if opts.tags != "enterprise,cuda" || opts.output != "./pando-enterprise" || opts.ldflags != "-X main.X=1" {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestParseArgsErrors(t *testing.T) {
	tests := map[string][]string{
		"no modules":        {},
		"unknown flag":      {"--with", "example.com/mod", "--nope", "x"},
		"missing value":     {"--with"},
		"two core versions": {"v1", "v2", "--with", "example.com/mod"},
		"bad replace":       {"--replace", "example.com/dep"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseArgs(args); err == nil {
				t.Fatalf("parseArgs(%v) = nil error", args)
			}
		})
	}
}

func TestParseArgsKeep(t *testing.T) {
	opts, err := parseArgs([]string{"--keep", "--with", "example.com/mod"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !opts.keep {
		t.Fatal("keep = false")
	}
}

func TestVariantValue(t *testing.T) {
	withMod := []module{{importPath: "example.com/mod"}}

	if got := (options{with: withMod}).variantValue(); got != "enterprise" {
		t.Fatalf("implied variant = %q, want enterprise", got)
	}
	if got := (options{with: withMod, variantSet: true, variant: "acme"}).variantValue(); got != "acme" {
		t.Fatalf("explicit variant = %q", got)
	}
	// An explicit empty --variant must be able to suppress the stamp.
	if got := (options{with: withMod, variantSet: true}).variantValue(); got != "" {
		t.Fatalf("suppressed variant = %q", got)
	}
	if got := (options{}).variantValue(); got != "" {
		t.Fatalf("plain build variant = %q", got)
	}
}

func TestLdflagsOrder(t *testing.T) {
	got := ldflags(options{with: []module{{importPath: "example.com/mod"}}, ldflags: "-s -w"})
	want := "-X " + versionPkg + ".Variant=enterprise -s -w"
	if got != want {
		t.Fatalf("ldflags = %q, want %q", got, want)
	}
	if got := ldflags(options{}); got != "" {
		t.Fatalf("ldflags = %q, want empty", got)
	}
}

func TestWriteMain(t *testing.T) {
	dir := t.TempDir()
	mods := []module{
		{importPath: "example.com/mod/tools"},
		{importPath: "example.com/other/api"},
	}
	if err := writeMain(dir, mods); err != nil {
		t.Fatalf("writeMain: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)

	for _, want := range []string{
		`pandocmd "github.com/digiogithub/pando/cmd"`,
		`_ "example.com/mod/tools"`,
		`_ "example.com/other/api"`,
		"pandocmd.Main()",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("generated main.go missing %q:\n%s", want, src)
		}
	}
}

func TestReplaced(t *testing.T) {
	local := options{with: []module{{modulePath: coreModule, localPath: "/tmp/core"}}}
	if !replaced(local, coreModule) {
		t.Fatal("a local --with must count as a replacement")
	}

	viaFlag := options{replaces: []replacement{{from: coreModule + "@v1.0.0", to: "../core"}}}
	if !replaced(viaFlag, coreModule) {
		t.Fatal("a versioned --replace must count as a replacement")
	}

	if replaced(options{with: []module{{modulePath: "example.com/mod"}}}, coreModule) {
		t.Fatal("unrelated modules must not count as a core replacement")
	}
}

func writeGoMod(t *testing.T, dir, module string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLdflagsStampsCoreVersion(t *testing.T) {
	opts := options{coreVersion: "v0.9.1", with: []module{{importPath: "example.com/mod"}}}
	want := "-X " + versionPkg + ".Variant=enterprise -X " + versionPkg + ".Version=v0.9.1"
	if got := ldflags(opts); got != want {
		t.Fatalf("ldflags = %q, want %q", got, want)
	}

	// An explicit version in --ldflags must not be duplicated or overridden.
	opts.ldflags = "-X " + versionPkg + ".Version=v1.0.0-rc1"
	got := ldflags(opts)
	if strings.Count(got, ".Version=") != 1 || !strings.Contains(got, "v1.0.0-rc1") {
		t.Fatalf("ldflags = %q", got)
	}
}
