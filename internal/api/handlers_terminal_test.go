package api

import (
	"bytes"
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDefaultShellExecArgsAreNotInteractive(t *testing.T) {
	args := defaultShellExecArgs()
	if slices.Contains(args, "-i") {
		t.Fatalf("web terminal runs without a PTY; an interactive shell gets stopped by SIGTTOU: %v", args)
	}
	if !slices.Contains(args, "-c") {
		t.Fatalf("shell must receive the command through -c: %v", args)
	}
}

func TestEnsureCommandFlag(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "adds -c when missing", in: []string{"-l"}, want: []string{"-l", "-c"}},
		{name: "keeps existing -c", in: []string{"-l", "-c"}, want: []string{"-l", "-c"}},
		{name: "empty args", in: nil, want: []string{"-c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureCommandFlag(tt.in); !slices.Equal(got, tt.want) {
				t.Fatalf("ensureCommandFlag(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestShellExecWithoutPTY runs the real shell selection the web terminal uses,
// with stdin detached exactly like the handler does, and asserts the command
// completes instead of hanging on job-control signals.
func TestShellExecWithoutPTY(t *testing.T) {
	shell, shellArgs := shellCommandForExec()
	args := append(append([]string(nil), shellArgs...), makeShellCommand(t.TempDir(), "echo pando-ok"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Env = buildShellEnv(nil)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v (output: %q)", shell, shellArgs, err, out.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("shell did not finish before the deadline: %v", ctx.Err())
	}
	if !strings.Contains(out.String(), "pando-ok") {
		t.Fatalf("missing command output, got %q", out.String())
	}
}
