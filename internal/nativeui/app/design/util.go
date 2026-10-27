package design

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func itoa(n int) string { return strconv.Itoa(n) }

// safeDate formats an RFC 3339 timestamp, or "" when it cannot be parsed
// (a row must render even if the server sent something odd).
func safeDate(value, layout string) string {
	for _, l := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, value); err == nil {
			return t.Local().Format(layout)
		}
	}
	return ""
}

// rebase makes a preview URL reachable from this client: the server builds
// absolute URLs on its own loopback address, which is wrong when the client
// reaches the server through another host (remote or proxied). The path and
// query are kept, the origin is the client's.
func rebase(raw, base string) string {
	if raw == "" || base == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return raw
	}
	if loopbackHost(u.Hostname()) && !loopbackHost(b.Hostname()) {
		u.Scheme, u.Host = b.Scheme, b.Host
		return u.String()
	}
	return raw
}

func loopbackHost(h string) bool {
	h = strings.ToLower(h)
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
