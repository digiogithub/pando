package outputfilter

import (
	"strings"
	"testing"
)

func TestGoTestJSONMatch(t *testing.T) {
	p := &goTestJSONParser{}
	if !p.Match("go test -json ./...") {
		t.Fatal("should match `go test -json`")
	}
	if p.Match("go test ./...") {
		t.Fatal("should not match plain `go test` (handled by TOML filter)")
	}
}

func TestGoTestJSONFullFailureFocus(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"run","Package":"pkg/a","Test":"TestOK"}`,
		`{"Action":"output","Package":"pkg/a","Test":"TestOK","Output":"=== RUN   TestOK\n"}`,
		`{"Action":"pass","Package":"pkg/a","Test":"TestOK"}`,
		`{"Action":"run","Package":"pkg/a","Test":"TestBad"}`,
		`{"Action":"output","Package":"pkg/a","Test":"TestBad","Output":"=== RUN   TestBad\n"}`,
		`{"Action":"output","Package":"pkg/a","Test":"TestBad","Output":"    a_test.go:12: want 1 got 2\n"}`,
		`{"Action":"output","Package":"pkg/a","Test":"TestBad","Output":"--- FAIL: TestBad (0.00s)\n"}`,
		`{"Action":"fail","Package":"pkg/a","Test":"TestBad"}`,
		`{"Action":"skip","Package":"pkg/a","Test":"TestSkip"}`,
		`{"Action":"fail","Package":"pkg/a"}`,
	}, "\n")

	res, tier := (&goTestJSONParser{}).parse(in)
	if tier != tierFull {
		t.Fatalf("expected tierFull, got %v", tier)
	}
	if !strings.Contains(res, "FAIL pkg/a · TestBad") {
		t.Errorf("missing failure header:\n%s", res)
	}
	if !strings.Contains(res, "a_test.go:12: want 1 got 2") {
		t.Errorf("missing failure detail:\n%s", res)
	}
	if strings.Contains(res, "TestOK") {
		t.Errorf("passing test should be summarized away:\n%s", res)
	}
	if !strings.Contains(res, "SUMMARY: 1 passed, 1 failed, 1 skipped") {
		t.Errorf("bad summary:\n%s", res)
	}
}

func TestGoTestJSONAllPass(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"pass","Package":"pkg/a","Test":"TestOne"}`,
		`{"Action":"pass","Package":"pkg/a","Test":"TestTwo"}`,
		`{"Action":"pass","Package":"pkg/a"}`,
	}, "\n")
	res, tier := (&goTestJSONParser{}).parse(in)
	if tier != tierFull {
		t.Fatalf("expected tierFull, got %v", tier)
	}
	if res != "SUMMARY: 2 passed (1 packages) — all pass" {
		t.Errorf("unexpected all-pass summary: %q", res)
	}
}

func TestGoTestJSONDegradesOnNonJSON(t *testing.T) {
	in := "# pkg/a\n./a.go:3:2: undefined: foo\nFAIL\tpkg/a [build failed]"
	res, tier := (&goTestJSONParser{}).parse(in)
	if tier != tierDegraded {
		t.Fatalf("expected tierDegraded, got %v", tier)
	}
	if !strings.Contains(res, "undefined: foo") {
		t.Errorf("degraded output should keep the build error:\n%s", res)
	}
}

func TestGolangciLintFullJSON(t *testing.T) {
	in := `{"Issues":[` +
		`{"FromLinter":"govet","Text":"shadow","Pos":{"Filename":"a.go","Line":10,"Column":2}},` +
		`{"FromLinter":"govet","Text":"unreachable","Pos":{"Filename":"a.go","Line":20,"Column":1}},` +
		`{"FromLinter":"staticcheck","Text":"SA1000","Pos":{"Filename":"b.go","Line":5,"Column":3}}` +
		`]}`
	res, tier := (&golangciLintParser{}).parse(in)
	if tier != tierFull {
		t.Fatalf("expected tierFull, got %v", tier)
	}
	if !strings.Contains(res, "a.go:10:2 [govet] shadow") {
		t.Errorf("missing issue line:\n%s", res)
	}
	if !strings.Contains(res, "SUMMARY: 3 issues across 2 files (govet: 2, staticcheck: 1)") {
		t.Errorf("bad summary:\n%s", res)
	}
}

func TestGolangciLintCleanJSON(t *testing.T) {
	res, tier := (&golangciLintParser{}).parse(`{"Issues":[]}`)
	if tier != tierFull || res != "SUMMARY: 0 issues — clean" {
		t.Errorf("expected clean summary, got tier=%v res=%q", tier, res)
	}
}

func TestGolangciLintDegradesOnText(t *testing.T) {
	in := "a.go:10:2: shadow declaration (govet)\nsome banner line\nb.go:5:3: SA1000 (staticcheck)"
	res, tier := (&golangciLintParser{}).parse(in)
	if tier != tierDegraded {
		t.Fatalf("expected tierDegraded, got %v", tier)
	}
	if strings.Contains(res, "banner") {
		t.Errorf("degraded output should drop the banner:\n%s", res)
	}
}

func TestTscFull(t *testing.T) {
	in := strings.Join([]string{
		"src/a.ts(12,5): error TS2304: Cannot find name 'foo'.",
		"src/a.ts(40,1): error TS2554: Expected 1 arguments, but got 0.",
		"src/b.ts(3,3): error TS7006: Parameter 'x' implicitly has an 'any' type.",
		"Found 3 errors.",
	}, "\n")
	res, tier := (&tscParser{}).parse(in)
	if tier != tierFull {
		t.Fatalf("expected tierFull, got %v", tier)
	}
	if !strings.Contains(res, "src/a.ts:12:5 [TS2304] Cannot find name 'foo'.") {
		t.Errorf("missing diagnostic:\n%s", res)
	}
	if !strings.Contains(res, "SUMMARY: 3 errors in 2 files") {
		t.Errorf("bad summary:\n%s", res)
	}
}

func TestTscMatch(t *testing.T) {
	p := &tscParser{}
	for _, cmd := range []string{"tsc --noEmit", "npx tsc", "node_modules/.bin/tsc -p ."} {
		if !p.Match(cmd) {
			t.Errorf("should match %q", cmd)
		}
	}
	if p.Match("gotsc-tool run") {
		t.Error("should not match an unrelated command containing 'tsc'")
	}
}

func TestEngineApplyPrefersParser(t *testing.T) {
	e, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	in := strings.Join([]string{
		`{"Action":"pass","Package":"pkg/a","Test":"TestOne"}`,
		`{"Action":"pass","Package":"pkg/a"}`,
	}, "\n")
	res, applied, name := e.Apply("go test -json ./...", in)
	if !applied || name != "go-test-json" {
		t.Fatalf("expected go-test-json parser, got applied=%v name=%q", applied, name)
	}
	if !strings.Contains(res, "all pass") {
		t.Errorf("parser result not used:\n%s", res)
	}
}

func TestEngineApplyPassthroughFallsThrough(t *testing.T) {
	// tsc matches but the output has no diagnostics and nothing to grep:
	// the parser should passthrough and Apply should report not-applied.
	e, err := LoadDefaults()
	if err != nil {
		t.Fatal(err)
	}
	raw := "Starting compilation...\nDone in 1.2s"
	res, applied, _ := e.Apply("tsc --noEmit", raw)
	if applied {
		t.Errorf("expected passthrough (not applied), got applied=true")
	}
	if res != raw {
		t.Errorf("passthrough must return raw output unchanged")
	}
}
