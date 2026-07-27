package mcpauth

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// BuildTLSConfig builds a *tls.Config for srv's TLS/mTLS options
// (Auth.ClientCert/ClientKey/ClientKeyPassword/CACert/SkipTLSVerify), or
// returns (nil, nil) when none of those fields are set — the common case,
// meaning "use whatever default transport the caller already has".
//
// srv should already have its secrets resolved via
// config.ResolveMCPServerSecrets so that Auth.ClientKeyPassword is
// plaintext; BuildTLSConfig does no decryption of its own.
//
// This lives in internal/mcpauth rather than internal/mcpclient so that
// internal/mcpauth/login.go (the interactive OAuth flow) can also apply it
// to the OAuthHandler's own HTTP client without creating an import cycle —
// internal/mcpclient already imports internal/mcpauth, so the reverse
// dependency is not available.
func BuildTLSConfig(srv config.MCPServer) (*tls.Config, error) {
	auth := srv.Auth
	if auth == nil {
		return nil, nil
	}
	if strings.TrimSpace(auth.ClientCert) == "" && strings.TrimSpace(auth.ClientKey) == "" &&
		strings.TrimSpace(auth.CACert) == "" && !auth.SkipTLSVerify &&
		strings.TrimSpace(auth.TLSServerName) == "" &&
		strings.TrimSpace(auth.MinTLSVersion) == "" && strings.TrimSpace(auth.MaxTLSVersion) == "" {
		return nil, nil
	}

	cfg := &tls.Config{}

	if strings.TrimSpace(auth.ClientCert) != "" || strings.TrimSpace(auth.ClientKey) != "" {
		if strings.TrimSpace(auth.ClientCert) == "" || strings.TrimSpace(auth.ClientKey) == "" {
			// config.Validate should already have caught this, but
			// BuildTLSConfig is also reachable from paths (tests, direct
			// callers) that may not have run Validate first.
			return nil, fmt.Errorf("mcpauth: MCP server TLS config requires both ClientCert and ClientKey")
		}
		cert, err := loadClientCertificate(expandPath(auth.ClientCert), expandPath(auth.ClientKey), auth.ClientKeyPassword)
		if err != nil {
			return nil, fmt.Errorf("mcpauth: load MCP client certificate/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if strings.TrimSpace(auth.CACert) != "" {
		pool, err := loadCACertPool(expandPath(auth.CACert), auth.CACertExclusive)
		if err != nil {
			return nil, fmt.Errorf("mcpauth: load MCP CA certificate: %w", err)
		}
		cfg.RootCAs = pool
	}

	if name := strings.TrimSpace(auth.TLSServerName); name != "" {
		// Corporate deployments routinely reach an MCP gateway through an IP
		// address, an internal alias or a tunnel whose hostname does not match
		// the certificate; overriding the SNI/verification name keeps full
		// certificate validation instead of forcing SkipTLSVerify.
		cfg.ServerName = name
	}

	if v := strings.TrimSpace(auth.MinTLSVersion); v != "" {
		parsed, err := parseTLSVersion(v)
		if err != nil {
			return nil, fmt.Errorf("mcpauth: MinTLSVersion: %w", err)
		}
		cfg.MinVersion = parsed
	}
	if v := strings.TrimSpace(auth.MaxTLSVersion); v != "" {
		parsed, err := parseTLSVersion(v)
		if err != nil {
			return nil, fmt.Errorf("mcpauth: MaxTLSVersion: %w", err)
		}
		cfg.MaxVersion = parsed
	}
	if cfg.MinVersion != 0 && cfg.MaxVersion != 0 && cfg.MinVersion > cfg.MaxVersion {
		return nil, fmt.Errorf("mcpauth: MinTLSVersion (%s) is higher than MaxTLSVersion (%s)", auth.MinTLSVersion, auth.MaxTLSVersion)
	}

	if auth.SkipTLSVerify {
		logging.Warn("mcpauth: SkipTLSVerify is enabled for an MCP server; TLS certificate validation is DISABLED for this connection, which is insecure and should only be used for local testing against a known-trusted server", "server", srv.URL)
		cfg.InsecureSkipVerify = true //nolint:gosec // explicit, logged, opt-in operator choice
	}

	return cfg, nil
}

// expandPath expands a leading "~" (home directory) and any $VAR / ${VAR}
// environment references in p. An empty p is returned unchanged.
func expandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	p = os.ExpandEnv(p)
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// loadClientCertificate reads and parses a client certificate/key pair for
// mTLS. When password is non-empty, the key is decrypted first.
func loadClientCertificate(certPath, keyPath, password string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read client certificate %q: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read client key %q: %w", keyPath, err)
	}

	if strings.TrimSpace(password) == "" {
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("parse client certificate/key pair: %w", err)
		}
		return cert, nil
	}

	decryptedKeyPEM, err := decryptPEMKey(keyPEM, password)
	if err != nil {
		return tls.Certificate{}, err
	}
	cert, err := tls.X509KeyPair(certPEM, decryptedKeyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse client certificate/decrypted key pair: %w", err)
	}
	return cert, nil
}

// decryptPEMKey decrypts every private-key PEM block in keyPEM using
// password, re-encoding each as an unencrypted PEM block so the result can
// be fed straight to tls.X509KeyPair.
//
// Go's standard library (crypto/x509) can only decrypt the legacy RFC 1423
// PEM encryption used by older OpenSSL "-----BEGIN RSA PRIVATE KEY-----"
// blocks with a DEK-Info header (via the deprecated
// IsEncryptedPEMBlock/DecryptPEMBlock pair, which still work despite the
// deprecation). It has no support for PKCS#8 "ENCRYPTED PRIVATE KEY"
// blocks (PBES2), which is what `openssl genpkey`/most modern tooling
// produces by default — that case returns a clear, actionable error
// instead of silently failing or panicking.
func decryptPEMKey(keyPEM []byte, password string) ([]byte, error) {
	var out []byte
	rest := keyPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if !strings.Contains(block.Type, "PRIVATE KEY") {
			out = append(out, pem.EncodeToMemory(block)...)
			continue
		}
		switch {
		case x509.IsEncryptedPEMBlock(block): //nolint:staticcheck // legacy RFC1423 PEM is the only stdlib-supported encrypted-key format
			der, derErr := x509.DecryptPEMBlock(block, []byte(password)) //nolint:staticcheck
			if derErr != nil {
				return nil, fmt.Errorf("decrypt client key with ClientKeyPassword: %w", derErr)
			}
			out = append(out, pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der})...)
		case block.Type == "ENCRYPTED PRIVATE KEY":
			der, decErr := decryptPKCS8PrivateKey(block.Bytes, password)
			if decErr != nil {
				return nil, fmt.Errorf("decrypt PKCS#8 client key with ClientKeyPassword: %w", decErr)
			}
			out = append(out, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})...)
		default:
			// Not actually encrypted (no DEK-Info header, not a PKCS#8
			// "ENCRYPTED PRIVATE KEY" block) even though ClientKeyPassword
			// was set; pass it through unchanged rather than erroring, since
			// the key itself is perfectly usable.
			out = append(out, pem.EncodeToMemory(block)...)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no PEM blocks found in client key")
	}
	return out, nil
}

// loadCACertPool builds the root pool used to verify the MCP server.
//
// By default the configured CA is ADDED to the host's system roots, because
// the common enterprise case is an internal CA fronting one MCP gateway while
// every other TLS dependency (the authorization server, an SSO domain, a CDN)
// still chains to public roots. Setting exclusive pins verification to the
// configured CA alone — stricter, and what to use when the server must never
// be accepted under any publicly-trusted chain.
func loadCACertPool(path string, exclusive bool) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate %q: %w", path, err)
	}

	var pool *x509.CertPool
	if exclusive {
		pool = x509.NewCertPool()
	} else {
		pool, err = x509.SystemCertPool()
		if err != nil || pool == nil {
			// Windows historically, and any host with an unreadable trust
			// store, land here: fall back to a CA-only pool rather than
			// failing the connection outright, and say so.
			logging.Warn("mcpauth: could not load the system certificate pool, trusting only the configured CACert", "error", err)
			pool = x509.NewCertPool()
		}
	}

	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no valid certificates found in CA certificate file %q", path)
	}
	return pool, nil
}

// parseTLSVersion maps a human-written TLS version ("1.2", "TLS1.3",
// "tls1.2") to its crypto/tls constant. Only 1.2 and 1.3 are accepted: 1.0
// and 1.1 are deprecated (RFC 8996) and Go's client no longer negotiates them
// by default, so silently accepting them would promise something that does
// not work.
func parseTLSVersion(v string) (uint16, error) {
	normalized := strings.ToLower(strings.TrimSpace(v))
	normalized = strings.TrimPrefix(normalized, "tls")
	normalized = strings.TrimPrefix(normalized, "v")
	normalized = strings.TrimSpace(normalized)
	switch normalized {
	case "1.2", "12":
		return tls.VersionTLS12, nil
	case "1.3", "13":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS version %q (supported: \"1.2\", \"1.3\")", v)
	}
}

// TransportForTLS returns an *http.Transport configured with tlsCfg, cloned
// from http.DefaultTransport so connection pooling/proxy/timeouts behave
// exactly like the library defaults elsewhere. It returns nil when tlsCfg is
// nil, so callers can treat a nil result as "no custom transport needed"
// (e.g. NewScopeCapturingTransport(nil) already falls back to
// http.DefaultTransport, and an http.Client with a nil Transport already
// uses http.DefaultTransport).
func TransportForTLS(tlsCfg *tls.Config) http.RoundTripper {
	if tlsCfg == nil {
		return nil
	}
	var t *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		t = base.Clone()
	} else {
		t = &http.Transport{}
	}
	t.TLSClientConfig = tlsCfg
	return t
}

// HTTPClientForTLS returns an *http.Client using TransportForTLS(tlsCfg) (a
// nil Transport falls back to http.DefaultTransport, matching the zero-value
// http.Client behavior) with the given timeout.
func HTTPClientForTLS(tlsCfg *tls.Config, timeout time.Duration) *http.Client {
	return &http.Client{Transport: TransportForTLS(tlsCfg), Timeout: timeout}
}
