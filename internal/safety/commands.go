package safety

import "strings"

var defaultDangerousShellPatterns = []string{
	"rm -rf /",
	"rm -fr /",
	"sudo",
	"chmod 777 /",
	"chmod -r 777 /",
	"dd if=",
	"mkfs",
	":(){ :|:& };:",
	"> /dev/sda",
	"shred /dev/",
	"wipefs",
	"parted /dev/",
	"fdisk /dev/",
}

// IsDangerousShellCommand reports whether a shell command matches one of the
// built-in dangerous patterns or any additional configured patterns.
func IsDangerousShellCommand(command string, additionalPatterns []string) bool {
	normalized := normalizeShellCommand(command)
	if normalized == "" {
		return false
	}

	for _, pattern := range defaultDangerousShellPatterns {
		if hasDangerousSegment(normalized, normalizeShellCommand(pattern)) {
			return true
		}
	}

	for _, pattern := range additionalPatterns {
		pattern = normalizeShellCommand(pattern)
		if pattern == "" {
			continue
		}
		if strings.Contains(normalized, pattern) {
			return true
		}
	}

	return false
}

func normalizeShellCommand(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	return strings.Join(strings.Fields(value), " ")
}

func hasDangerousSegment(command string, pattern string) bool {
	if pattern == "" {
		return false
	}
	if segmentStartsWith(command, pattern) {
		return true
	}
	for _, separator := range []string{" && ", " || ", "; ", "| ", "\n"} {
		if strings.Contains(command, separator+pattern) {
			return true
		}
	}
	return false
}

func segmentStartsWith(segment string, pattern string) bool {
	if segment == pattern {
		return true
	}
	if len(segment) <= len(pattern) || !strings.HasPrefix(segment, pattern) {
		return false
	}
	switch segment[len(pattern)] {
	case ' ', '\t', '\n':
		return true
	}
	return false
}
