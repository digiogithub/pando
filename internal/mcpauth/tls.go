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
		strings.TrimSpace(auth.CACert) == "" && !auth.SkipTLSVerify {
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
		pool, err := loadCACertPool(expandPath(auth.CACert))
		if err != nil {
			return nil, fmt.Errorf("mcpauth: load MCP CA certificate: %w", err)
		}
		cfg.RootCAs = pool
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
			return nil, fmt.Errorf("client key is a PKCS#8 encrypted private key (PBES2), which Go's standard library cannot decrypt; decrypt it out-of-band first (e.g. `openssl pkcs8 -in key.pem -out key-decrypted.pem`) and point ClientKey at the decrypted file, or remove ClientKeyPassword if the key is not actually encrypted")
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

func loadCACertPool(path string) (*x509.CertPool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate %q: %w", path, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("no valid certificates found in CA certificate file %q", path)
	}
	return pool, nil
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
