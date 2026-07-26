package mcpauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxIssMetadataBodyBytes caps the size of the raw AS metadata document
// fetched solely to read authorization_response_iss_parameter_supported.
const maxIssMetadataBodyBytes = 1 << 20 // 1 MiB

// errIssMissing is wrapped into the error returned by validateIssuer when
// the AS metadata declares authorization_response_iss_parameter_supported
// (RFC 9207 §3) but the authorization response omitted iss entirely.
var errIssMissing = errors.New("mcpauth: authorization server declares authorization_response_iss_parameter_supported but the callback did not include an iss parameter")

// errIssMismatch is wrapped into the error returned by validateIssuer when
// the callback's iss parameter does not exactly match the issuer recorded
// from the validated AS metadata before the user was redirected.
var errIssMismatch = errors.New("mcpauth: authorization response iss does not match the expected issuer (possible authorization server mix-up attack)")

// validateIssuer implements the RFC 9207 §3 validation table for the
// authorization response's iss parameter:
//
//   - supported==true,  got=="" : REJECT (errIssMissing) — the AS promised
//     iss but didn't send it.
//   - supported==true,  got!="" : exact string match against expected, or
//     REJECT (errIssMismatch).
//   - supported==false, got=="" : proceed (nil) — nothing to validate.
//   - supported==false, got!="" : compare anyway (local policy the MCP spec
//     explicitly permits) — exact match against expected, or REJECT.
//
// The comparison is RFC 3986 §6.2.1 "simple string comparison": no case
// folding, no default-port elision, no trailing-slash or percent-encoding
// normalization. Two issuer strings that a browser or a lenient URL parser
// would treat as equivalent (e.g. differing only by a trailing slash, a
// default port, or letter case in the scheme/host) are treated as a
// mismatch here, exactly as the authorization server's own issuer string
// must have been emitted byte-for-byte identically on both the metadata
// document and the callback's iss parameter for a legitimate flow.
func validateIssuer(expected, got string, supported bool) error {
	if got == "" {
		if supported {
			return errIssMissing
		}
		return nil
	}
	if got != expected {
		return fmt.Errorf("%w: expected %q, got %q", errIssMismatch, expected, got)
	}
	return nil
}

// issRawMetadata is the subset of the AS metadata document (RFC 8414 plus
// the RFC 9207 §3 flag) that mcpauth decodes itself.
type issRawMetadata struct {
	Issuer                                     string `json:"issuer"`
	AuthorizationResponseIssParameterSupported bool   `json:"authorization_response_iss_parameter_supported"`
}

// fetchIssSupport re-fetches the authorization server metadata document to
// read authorization_response_iss_parameter_supported, a field mcp-go
// v0.57.0's client/transport.AuthServerMetadata does not expose (verified
// against the vendored source: the struct models RFC 8414's fields only).
// This performs a second network round trip beyond mcp-go's own internal
// discovery — an accepted, documented workaround given mcp-go does not
// expose its raw decoded JSON or the exact URL that answered discovery.
//
// metadataURL, when non-empty, is used directly (mirrors an explicitly
// configured Auth.OAuth.AuthServerMetadataURL, or a handler that already
// resolved one). Otherwise issDiscoveryCandidateURLs derives the same
// well-known discovery URLs from issuer that mcp-go's own auto-discovery
// tries, and the first that answers 200 with valid JSON wins.
//
// Any failure (network error, non-200, malformed JSON) degrades to
// supported=false rather than propagating: the worst case is that iss
// validation falls back to the always-safe "compare-if-present" policy for
// this login, not a hard failure of the entire flow over a metadata-fetch
// hiccup that mcp-go's own primary discovery may not have hit.
func fetchIssSupport(ctx context.Context, httpClient *http.Client, metadataURL, issuer string) bool {
	var urls []string
	if strings.TrimSpace(metadataURL) != "" {
		urls = []string{metadataURL}
	} else {
		urls = issDiscoveryCandidateURLs(issuer)
	}
	for _, u := range urls {
		if supported, ok := fetchOneIssSupport(ctx, httpClient, u); ok {
			return supported
		}
	}
	return false
}

func fetchOneIssSupport(ctx context.Context, httpClient *http.Client, metadataURL string) (supported bool, ok bool) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return false, false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var doc issRawMetadata
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxIssMetadataBodyBytes))
	if err := dec.Decode(&doc); err != nil {
		return false, false
	}
	return doc.AuthorizationResponseIssParameterSupported, true
}

// issDiscoveryCandidateURLs mirrors mcp-go's unexported
// authorizationServerMetadataURLs (client/transport/oauth.go): RFC 8414 §3
// path-insertion semantics plus OpenID Connect Discovery 1.0 fallbacks. It
// is duplicated here (rather than reused) because mcp-go does not export
// it, and mcpauth needs the exact same candidate order to have a realistic
// chance of hitting the same document mcp-go's own discovery found.
func issDiscoveryCandidateURLs(issuerURL string) []string {
	u, err := url.Parse(issuerURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil
	}
	u.RawQuery = ""
	u.Fragment = ""

	originalPath := strings.Trim(u.Path, "/")

	var urls []string
	if originalPath == "" {
		u.Path = "/.well-known/oauth-authorization-server"
		urls = append(urls, u.String())
		u.Path = "/.well-known/openid-configuration"
		urls = append(urls, u.String())
		return urls
	}
	u.Path = "/.well-known/oauth-authorization-server/" + originalPath
	urls = append(urls, u.String())
	u.Path = "/.well-known/openid-configuration/" + originalPath
	urls = append(urls, u.String())
	u.Path = "/" + originalPath + "/.well-known/openid-configuration"
	urls = append(urls, u.String())
	return urls
}
