package convert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrUnsupportedFormat is returned when the document's format cannot be
// converted.
var ErrUnsupportedFormat = errors.New("convert: unsupported document format")

// ErrConversionFailed is returned when the converter could not read the
// document (broken, encrypted or not what its type claims).
var ErrConversionFailed = errors.New("convert: conversion failed")

// ErrOutputTooLarge is returned when the Markdown exceeds the output limit.
var ErrOutputTooLarge = errors.New("convert: markdown exceeds the output limit")

// Runner converts one in-memory document to Markdown. ext is the canonical
// extension of the document's format (".pdf", ".docx", …), chosen by the
// caller from an allow-list, never taken verbatim from a client.
type Runner interface {
	Run(ctx context.Context, ext string, data []byte, maxOutput int64) (string, error)
}

// InProcessRunner converts in the calling process with a shared Converter.
// The conversion library cannot be interrupted, so on a timeout the call
// returns while the conversion keeps running (and holding the Converter's
// lock) until it finishes. Use it for trusted input and tests; ProcessRunner
// is the isolated choice for untrusted documents.
type InProcessRunner struct {
	Converter *Converter
}

// Run implements Runner.
func (r InProcessRunner) Run(ctx context.Context, ext string, data []byte, maxOutput int64) (string, error) {
	type result struct {
		md  string
		err error
	}
	done := make(chan result, 1)
	go func() {
		md, err := r.Converter.ConvertReader(bytes.NewReader(data), "document"+ext)
		done <- result{md, err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-done:
		if res.err != nil {
			return "", fmt.Errorf("%w: %v", ErrConversionFailed, res.err)
		}
		if maxOutput > 0 && int64(len(res.md)) > maxOutput {
			return "", ErrOutputTooLarge
		}
		return res.md, nil
	}
}

// ProcessRunner converts each document in a child process: it runs
// `<Executable> convert --stdin-format <ext>`, writes the document to the
// child's stdin and reads the Markdown from its stdout. The child is killed
// when the context ends or its output passes the limit, so a hostile
// document cannot hang or exhaust the server. No shell is involved, the only
// variable argument is the allow-listed extension, the child gets a minimal
// environment (no inherited secrets) and nothing is written to disk.
type ProcessRunner struct {
	// Executable is the Pando binary; empty means os.Executable().
	Executable string
	// Args replaces the default ["convert", "--stdin-format"] prefix; the
	// extension is appended. Tests use it to re-exec the test binary.
	Args []string
	// Env is the child's whole environment; nil means MinimalEnv(MemoryLimit).
	Env []string
	// MemoryLimit is the child's GOMEMLIMIT (a soft limit, e.g. "1GiB");
	// empty sets none. Hard limits belong to the container or cgroup.
	MemoryLimit string
}

// MinimalEnv is the environment given to a conversion child: PATH, a temp
// directory and, when set, GOMEMLIMIT. Nothing else of the parent leaks in.
func MinimalEnv(memoryLimit string) []string {
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin", "TMPDIR=" + os.TempDir(), "HOME=" + os.TempDir()}
	if memoryLimit != "" {
		env = append(env, "GOMEMLIMIT="+memoryLimit)
	}
	return env
}

// Run implements Runner.
func (r ProcessRunner) Run(ctx context.Context, ext string, data []byte, maxOutput int64) (string, error) {
	exe := r.Executable
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return "", fmt.Errorf("convert: locate executable: %w", err)
		}
	}
	args := r.Args
	if args == nil {
		args = []string{"convert", "--stdin-format"}
	}
	args = append(append([]string(nil), args...), ext)
	env := r.Env
	if env == nil {
		env = MinimalEnv(r.MemoryLimit)
	}

	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = env
	cmd.Dir = os.TempDir()
	cmd.Stdin = bytes.NewReader(data)
	stdout := &cappedBuffer{limit: maxOutput, onOverflow: cancel}
	stderr := &cappedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	switch {
	case stdout.overflow:
		return "", ErrOutputTooLarge
	case parent.Err() != nil:
		// Deadline reached or client gone: the child was killed.
		return "", parent.Err()
	case err != nil:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%w: %s", ErrConversionFailed, firstLine(stderr.String()))
		}
		return "", fmt.Errorf("convert: run child: %w", err)
	}
	// `pando convert` ends its output with one newline of its own.
	return strings.TrimSuffix(stdout.String(), "\n"), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// cappedBuffer collects at most limit bytes (0 means no limit). Past the
// limit it drops data, marks overflow and calls onOverflow once.
type cappedBuffer struct {
	buf        bytes.Buffer
	limit      int64
	overflow   bool
	onOverflow func()
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && int64(b.buf.Len()+len(p)) > b.limit {
		room := int(b.limit) - b.buf.Len()
		if room > 0 {
			b.buf.Write(p[:room])
		}
		if !b.overflow {
			b.overflow = true
			if b.onOverflow != nil {
				b.onOverflow()
			}
		}
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string { return b.buf.String() }
