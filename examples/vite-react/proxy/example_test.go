package main

import (
	"fmt"
	"net/url"
)

// Example_reverseProxy compiles the reverse-proxy snippet mirrored in
// internal/agui/doc.go and sdk/typescript/README.md, so that documentation
// cannot silently rot: `go test ./...` fails to even build the moment
// newReverseProxy's signature, or httputil.ReverseProxy's own API, changes
// underneath it.
func Example_reverseProxy() {
	target, err := url.Parse("https://localhost:8090")
	if err != nil {
		panic(err)
	}
	proxy := newReverseProxy(target, "example-token")

	// FlushInterval must be negative ("flush after every write, never
	// batch") -- the property the doc.go "Streaming" section documents as
	// required for SSE to reach the client promptly.
	fmt.Println(proxy.FlushInterval < 0)
	fmt.Println(proxy.Director != nil)
	// Output:
	// true
	// true
}
