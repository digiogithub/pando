package desktop

import "fmt"

// configScript returns the JavaScript snippet that exposes the Pando backend
// URL and auth token to the web frontend as window.__PANDO__.
func configScript(serverURL, token string) string {
	return fmt.Sprintf(`window.__PANDO__ = { apiBase: %q, token: %q };`, serverURL, token)
}
