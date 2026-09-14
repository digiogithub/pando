// Command proxy is the only backend hop the vite-react example needs: a
// small Go reverse proxy that sits between the browser and
// `pando agui-serve`, implementing the reverse-proxy contract documented in
// ../../../internal/agui/doc.go ("Reverse-proxy contract") and mirrored in
// sdk/typescript/README.md.
//
// It does five things, each pinned to the doc.go fact it satisfies:
//
//  1. Strips the browser's Origin header before forwarding upstream, so
//     Pando's authorize() (internal/agui/server.go:71-78) never sees one and
//     AGUIConfig.AllowedOrigins can stay empty — the recommended shape for a
//     server-to-server proxy.
//  2. Strips any inbound "?token=" and sets the real bearer token itself
//     (internal/agui/server.go:97-103), so the Pando token never has to
//     reach the browser at all.
//  3. Sets FlushInterval: -1 and imposes no write/idle timeout below the
//     adapter's 15s heartbeat, so SSE deltas are not buffered or cut off
//     mid-stream (internal/agui/sse.go, internal/agui/deps.go:162,
//     internal/agui/listener.go:88-92).
//  4. Adds its own (separate) CORS policy for the browser-facing hop — this
//     has nothing to do with Pando's Origin allow-list, since Pando never
//     sees the browser's Origin once this proxy is in front of it.
//  5. Never terminates TLS for the upstream hop itself: run `pando agui-serve
//     --no-tls` on loopback behind this proxy (the pragmatic choice for a
//     local demo); across any other network boundary, pin the certificate
//     instead of disabling TLS.
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

// newReverseProxy builds the reverse proxy that fronts a `pando agui-serve`
// instance at target, injecting pandoToken as the bearer credential on every
// forwarded request. This is the snippet mirrored in
// sdk/typescript/README.md; example_test.go compiles it so it cannot rot.
func newReverseProxy(target *url.URL, pandoToken string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Flush after every write instead of batching: buffering here would
	// hold back SSE deltas exactly like an nginx/Envoy hop without an
	// equivalent setting.
	proxy.FlushInterval = -1

	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)

		// Never forward the browser's own Origin upstream: an absent
		// Origin makes authorize() skip the allow-list entirely
		// (internal/agui/server.go:71-78), which is the recommended
		// shape for a server-to-server proxy.
		req.Header.Del("Origin")

		// Strip any inbound ?token= (it would otherwise sit in access
		// logs, Referer headers and browser history) and set the real
		// bearer token here instead, so it never has to reach the
		// browser at all.
		q := req.URL.Query()
		q.Del("token")
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Authorization", "Bearer "+pandoToken)
	}

	return proxy
}

// browserOrigin is the origin this proxy allows the BROWSER to call IT from.
// This is the proxy's own, ordinary CORS policy — a separate decision from
// Pando's Origin allow-list above, which this proxy's upstream hop never
// exercises (Origin is stripped before the request reaches Pando).
func browserOrigin() string {
	if v := os.Getenv("VITE_REACT_ORIGIN"); v != "" {
		return v
	}
	return "http://localhost:5173" // Vite's default dev server origin.
}

func withCORS(origin string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func main() {
	pandoURL := os.Getenv("PANDO_AGUI_URL")
	if pandoURL == "" {
		pandoURL = "http://localhost:8090" // matches `pando agui-serve --no-tls`
	}
	target, err := url.Parse(pandoURL)
	if err != nil {
		log.Fatalf("invalid PANDO_AGUI_URL %q: %v", pandoURL, err)
	}

	token := os.Getenv("PANDO_AGUI_TOKEN")
	if token == "" {
		log.Fatal("PANDO_AGUI_TOKEN is required: the bearer token `pando agui-serve` printed on startup")
	}

	addr := os.Getenv("PROXY_ADDR")
	if addr == "" {
		addr = ":8091"
	}

	handler := withCORS(browserOrigin(), newReverseProxy(target, token))
	srv := &http.Server{
		Addr:        addr,
		Handler:     handler,
		ReadTimeout: 30 * time.Second,
		// No write timeout: an AG-UI run streams SSE for as long as the
		// agent takes, exactly like the adapter's own dedicated listener
		// (internal/agui/listener.go:88-92) — a proxy that imposes one
		// below the 15s heartbeat would read a slow tool call as a dead
		// connection.
		WriteTimeout: 0,
		IdleTimeout:  90 * time.Second,
	}

	log.Printf("proxy listening on %s -> %s (browser origin: %s)", addr, target, browserOrigin())
	log.Fatal(srv.ListenAndServe())
}
