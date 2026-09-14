package config

// TestingT is the subset of *testing.T that IsolateForTests needs. It is
// declared here rather than importing "testing" so that this file, which is
// part of the normal package build, does not pull the testing package into
// production binaries.
type TestingT interface {
	Helper()
	Setenv(key, value string)
	TempDir() string
	Cleanup(f func())
}

// IsolateForTests gives the calling test its own configuration environment.
//
// Configuration is a process-wide singleton: Load reads the real HOME and XDG
// config, caches the result in a package global and hands every later
// config.Get() that same value. A test that calls Load therefore leaks its
// configuration into every test that runs after it in the same binary, which
// is how four tests in internal/llm/agent came to pass in isolation and fail
// when their package ran as a whole (PANDO-T-0003).
//
// This points HOME at a scratch directory so Load cannot find a host config,
// clears XDG_CONFIG_HOME so it cannot find one there either, and resets the
// singleton both before the test runs and after it finishes. Call it first in
// any test that reaches configuration, in this package or any other.
func IsolateForTests(t TestingT) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	ResetForTests()
	t.Cleanup(ResetForTests)
}
