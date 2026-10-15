package llmproxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strings"
)

// APIKeyEnv is the environment variable that supplies the proxy API key when
// neither --api-key nor --api-key-file is given.
const APIKeyEnv = "PANDO_LLM_PROXY_API_KEY"

// ResolveAPIKey picks the proxy API key from, in order of precedence, the
// --api-key flag value, the file at keyFile, then the PANDO_LLM_PROXY_API_KEY
// environment variable (read through getenv; nil means os.Getenv). Passing both
// flagKey and keyFile is an error. The file is read once and one trailing
// newline (LF or CRLF) is trimmed; an unreadable or empty file is an error. An
// empty result with a nil error means authentication is disabled.
func ResolveAPIKey(flagKey, keyFile string, getenv func(string) string) (string, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	if flagKey != "" && keyFile != "" {
		return "", errors.New("--api-key and --api-key-file are mutually exclusive")
	}
	if flagKey != "" {
		return flagKey, nil
	}
	if keyFile != "" {
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return "", fmt.Errorf("read --api-key-file: %w", err)
		}
		key := strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r")
		if key == "" {
			return "", fmt.Errorf("--api-key-file %s is empty", keyFile)
		}
		return key, nil
	}
	return getenv(APIKeyEnv), nil
}

// keyMatches compares the presented token with the configured key in constant
// time. Both values are hashed with SHA-256 first so that the comparison time
// depends neither on the content nor on the length of the configured key.
func keyMatches(want, got string) bool {
	w := sha256.Sum256([]byte(want))
	g := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(w[:], g[:]) == 1
}
