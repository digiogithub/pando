package safety

import "testing"

func TestIsDangerousShellCommandMatchesBuiltInPatternAcrossChainedCommands(t *testing.T) {
	if !IsDangerousShellCommand("echo ok && rm -rf /tmp/build-cache", nil) {
		t.Fatal("expected chained dangerous command to match built-in patterns")
	}
}

func TestIsDangerousShellCommandMatchesConfiguredPattern(t *testing.T) {
	if !IsDangerousShellCommand("sqlite3 app.db 'DROP TABLE users;'", []string{"DROP TABLE"}) {
		t.Fatal("expected configured dangerous pattern to match command")
	}
}
