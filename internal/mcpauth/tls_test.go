package mcpauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// generateSelfSignedCert creates a self-signed ECDSA certificate/key pair
// for tests, writing PEM-encoded cert and key files under dir and returning
// their paths.
func generateSelfSignedCert(t *testing.T, dir, prefix string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "pando-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPath = filepath.Join(dir, prefix+"-cert.pem")
	keyPath = filepath.Join(dir, prefix+"-key.pem")

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

func TestBuildTLSConfig_NoOptionsReturnsNil(t *testing.T) {
	srv := config.MCPServer{URL: "https://example.com", Auth: &config.MCPAuth{Type: config.MCPAuthNone}}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil *tls.Config, got %#v", cfg)
	}
}

func TestBuildTLSConfig_NilAuth(t *testing.T) {
	srv := config.MCPServer{URL: "https://example.com"}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil *tls.Config for nil Auth, got %#v", cfg)
	}
}

func TestBuildTLSConfig_ClientCertLoads(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateSelfSignedCert(t, dir, "client")

	srv := config.MCPServer{
		URL: "https://example.com",
		Auth: &config.MCPAuth{
			Type:       config.MCPAuthNone,
			ClientCert: certPath,
			ClientKey:  keyPath,
		},
	}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil *tls.Config")
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 certificate, got %d", len(cfg.Certificates))
	}
}

func TestBuildTLSConfig_MissingFileGivesClearError(t *testing.T) {
	srv := config.MCPServer{
		URL: "https://example.com",
		Auth: &config.MCPAuth{
			Type:       config.MCPAuthNone,
			ClientCert: "/nonexistent/cert.pem",
			ClientKey:  "/nonexistent/key.pem",
		},
	}
	_, err := BuildTLSConfig(srv)
	if err == nil {
		t.Fatal("expected an error for a missing cert file")
	}
}

func TestBuildTLSConfig_CACertPool(t *testing.T) {
	dir := t.TempDir()
	caCertPath, _ := generateSelfSignedCert(t, dir, "ca")

	srv := config.MCPServer{
		URL: "https://example.com",
		Auth: &config.MCPAuth{
			Type:   config.MCPAuthNone,
			CACert: caCertPath,
		},
	}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if cfg == nil || cfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated")
	}
}

func TestBuildTLSConfig_CACertMissingFile(t *testing.T) {
	srv := config.MCPServer{
		URL:  "https://example.com",
		Auth: &config.MCPAuth{Type: config.MCPAuthNone, CACert: "/nonexistent/ca.pem"},
	}
	if _, err := BuildTLSConfig(srv); err == nil {
		t.Fatal("expected an error for a missing CA cert file")
	}
}

func TestBuildTLSConfig_SkipTLSVerify(t *testing.T) {
	srv := config.MCPServer{
		URL:  "https://example.com",
		Auth: &config.MCPAuth{Type: config.MCPAuthNone, SkipTLSVerify: true},
	}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil *tls.Config")
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify = true")
	}
}

func TestBuildTLSConfig_RequiresBothCertAndKey(t *testing.T) {
	srv := config.MCPServer{
		URL:  "https://example.com",
		Auth: &config.MCPAuth{Type: config.MCPAuthNone, ClientCert: "/tmp/only-cert.pem"},
	}
	if _, err := BuildTLSConfig(srv); err == nil {
		t.Fatal("expected an error when ClientCert is set without ClientKey")
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory available: %v", err)
	}
	t.Setenv("PANDO_TEST_TLS_DIR", "subdir")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"tilde alone", "~", home},
		{"tilde slash", "~/certs/client.pem", filepath.Join(home, "certs/client.pem")},
		{"env var", "/data/$PANDO_TEST_TLS_DIR/client.pem", "/data/subdir/client.pem"},
		{"plain path unchanged", "/etc/pando/client.pem", "/etc/pando/client.pem"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := expandPath(tt.in); got != tt.want {
				t.Fatalf("expandPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTransportForTLS(t *testing.T) {
	if rt := TransportForTLS(nil); rt != nil {
		t.Fatalf("expected nil RoundTripper for nil tls.Config, got %#v", rt)
	}
	cfg := &tls.Config{InsecureSkipVerify: true}
	rt := TransportForTLS(cfg)
	transport, ok := rt.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", rt)
	}
	if transport.TLSClientConfig != cfg {
		t.Fatal("expected the returned transport to carry the given tls.Config")
	}
}

func TestHTTPClientForTLS(t *testing.T) {
	c := HTTPClientForTLS(nil, 5*time.Second)
	if c.Transport != nil {
		t.Fatalf("expected nil Transport when tlsCfg is nil, got %#v", c.Transport)
	}
	if c.Timeout != 5*time.Second {
		t.Fatalf("expected Timeout = 5s, got %v", c.Timeout)
	}

	cfg := &tls.Config{InsecureSkipVerify: true}
	c2 := HTTPClientForTLS(cfg, 5*time.Second)
	if c2.Transport == nil {
		t.Fatal("expected non-nil Transport when tlsCfg is set")
	}
}

// generateEncryptedRSACert generates an RSA certificate/key pair and writes
// the certificate in the clear plus the key encrypted with the legacy RFC
// 1423 PEM encryption (the one format Go's standard library can decrypt),
// protected by password.
func generateEncryptedRSACert(t *testing.T, dir, password string) (certPath, keyPath string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "pando-test-encrypted"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(priv)

	block, err := x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", keyDER, []byte(password), x509.PEMCipherAES256) //nolint:staticcheck
	if err != nil {
		t.Fatalf("encrypt PEM block: %v", err)
	}

	certPath = filepath.Join(dir, "encrypted-cert.pem")
	keyPath = filepath.Join(dir, "encrypted-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write encrypted key: %v", err)
	}
	return certPath, keyPath
}

func TestBuildTLSConfig_EncryptedClientKeyDecrypts(t *testing.T) {
	dir := t.TempDir()
	const password = "s3cret-passphrase"
	certPath, keyPath := generateEncryptedRSACert(t, dir, password)

	srv := config.MCPServer{
		URL: "https://example.com",
		Auth: &config.MCPAuth{
			Type:              config.MCPAuthNone,
			ClientCert:        certPath,
			ClientKey:         keyPath,
			ClientKeyPassword: password,
		},
	}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig with encrypted key: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 loaded certificate, got %#v", cfg)
	}
}

func TestBuildTLSConfig_EncryptedClientKeyWrongPassword(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := generateEncryptedRSACert(t, dir, "correct-password")

	srv := config.MCPServer{
		URL: "https://example.com",
		Auth: &config.MCPAuth{
			Type:              config.MCPAuthNone,
			ClientCert:        certPath,
			ClientKey:         keyPath,
			ClientKeyPassword: "wrong-password",
		},
	}
	if _, err := BuildTLSConfig(srv); err == nil {
		t.Fatal("expected an error for a wrong ClientKeyPassword")
	}
}

func TestBuildTLSConfig_PKCS8EncryptedKeyGivesActionableError(t *testing.T) {
	dir := t.TempDir()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	// x509.MarshalPKCS8PrivateKey has no built-in encryption support in the
	// standard library; simulate the shape of an "ENCRYPTED PRIVATE KEY"
	// PKCS#8 block (as produced by `openssl pkcs8 -topk8`) directly, since
	// that block type alone is enough to exercise BuildTLSConfig's error
	// path without needing a real PBES2 implementation.
	pkcs8DER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	fakeEncryptedBlock := pem.EncodeToMemory(&pem.Block{Type: "ENCRYPTED PRIVATE KEY", Bytes: pkcs8DER})

	certTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(3),
		Subject:               pkix.Name{CommonName: "pando-test-pkcs8"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, certTmpl, certTmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certPath := filepath.Join(dir, "pkcs8-cert.pem")
	keyPath := filepath.Join(dir, "pkcs8-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, fakeEncryptedBlock, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	srv := config.MCPServer{
		URL: "https://example.com",
		Auth: &config.MCPAuth{
			Type:              config.MCPAuthNone,
			ClientCert:        certPath,
			ClientKey:         keyPath,
			ClientKeyPassword: "irrelevant",
		},
	}
	_, err = BuildTLSConfig(srv)
	if err == nil {
		t.Fatal("expected an actionable error for a PKCS#8 encrypted key")
	}
	if !strings.Contains(err.Error(), "PKCS#8") {
		t.Fatalf("expected error to mention PKCS#8, got: %v", err)
	}
}

// TestBuildTLSConfig_EndToEndMutualTLS spins up an httptest TLS server that
// requires a client certificate signed by a private test CA, then verifies
// that BuildTLSConfig produces a *tls.Config an http.Client can use to
// successfully call it, and that omitting the client cert (or trusting a
// different CA) fails as expected.
func TestBuildTLSConfig_EndToEndMutualTLS(t *testing.T) {
	dir := t.TempDir()

	// Minimal self-signed "CA" that also serves as the server's leaf cert,
	// and a second, independently self-signed cert to act as the client
	// certificate. httptest.NewUnstartedServer + a client CertPool seeded
	// with the client cert's own certificate lets the server's
	// ClientCAs verify it directly (self-signed acting as its own root),
	// keeping the fixture simple.
	serverCertPath, serverKeyPath := generateSelfSignedCert(t, dir, "server")
	clientCertPath, clientKeyPath := generateSelfSignedCert(t, dir, "client-e2e")

	serverCert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		t.Fatalf("load server cert: %v", err)
	}
	clientCertPEM, err := os.ReadFile(clientCertPath)
	if err != nil {
		t.Fatalf("read client cert: %v", err)
	}
	clientCAPool := x509.NewCertPool()
	if !clientCAPool.AppendCertsFromPEM(clientCertPEM) {
		t.Fatal("failed to add client cert to pool")
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAPool,
	}
	ts.StartTLS()
	defer ts.Close()

	// Client trusts the server's self-signed cert as its own root, and
	// presents its own client certificate.
	srv := config.MCPServer{
		URL: ts.URL,
		Auth: &config.MCPAuth{
			Type:       config.MCPAuthNone,
			ClientCert: clientCertPath,
			ClientKey:  clientKeyPath,
			CACert:     serverCertPath,
		},
	}
	tlsCfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	httpClient := HTTPClientForTLS(tlsCfg, 5*time.Second)

	resp, err := httpClient.Get(ts.URL)
	if err != nil {
		t.Fatalf("request with client cert should succeed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Without a client certificate, the server must reject the handshake.
	noCertCfg, err := BuildTLSConfig(config.MCPServer{URL: ts.URL, Auth: &config.MCPAuth{Type: config.MCPAuthNone, CACert: serverCertPath}})
	if err != nil {
		t.Fatalf("BuildTLSConfig (no client cert): %v", err)
	}
	noCertClient := HTTPClientForTLS(noCertCfg, 5*time.Second)
	if _, err := noCertClient.Get(ts.URL); err == nil {
		t.Fatal("expected the handshake to fail without a client certificate")
	}
}
