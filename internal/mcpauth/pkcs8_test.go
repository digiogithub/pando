package mcpauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// The fixtures under testdata/mtls were produced with OpenSSL 3.0 from one
// RSA key pair, so every encrypted variant must decrypt to the exact same
// PKCS#8 DER as plain.pem:
//
//	openssl req -x509 -newkey rsa:2048 -keyout plain.pem -out cert.pem \
//	    -days 3650 -nodes -subj "/CN=pando-mtls-test"
//	openssl pkcs8 -topk8 -in plain.pem -out enc-aes256-sha256.pem \
//	    -v2 aes-256-cbc -v2prf hmacWithSHA256 -passout pass:hunter2
//	openssl pkcs8 -topk8 -in plain.pem -out enc-aes128-sha1.pem \
//	    -v2 aes-128-cbc -v2prf hmacWithSHA1 -passout pass:hunter2
//	openssl pkcs8 -topk8 -in plain.pem -out enc-scrypt.pem -scrypt -passout pass:hunter2
//	openssl pkcs8 -topk8 -in plain.pem -out enc-des3.pem -v2 des-ede3-cbc -passout pass:hunter2
const fixturePassword = "hunter2"

func fixturePath(name string) string { return filepath.Join("testdata", "mtls", name) }

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// plainKeyDER returns the DER of the unencrypted reference key.
func plainKeyDER(t *testing.T) []byte {
	t.Helper()
	block, _ := pem.Decode(readFixture(t, "plain.pem"))
	if block == nil {
		t.Fatal("plain.pem contains no PEM block")
	}
	return block.Bytes
}

func TestDecryptPEMKeyPKCS8Variants(t *testing.T) {
	want := plainKeyDER(t)

	for _, name := range []string{
		"enc-aes256-sha256.pem",
		"enc-aes128-sha1.pem",
		"enc-scrypt.pem",
		"enc-des3.pem",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := decryptPEMKey(readFixture(t, name), fixturePassword)
			if err != nil {
				t.Fatalf("decryptPEMKey(%s): %v", name, err)
			}
			block, _ := pem.Decode(out)
			if block == nil {
				t.Fatal("decrypted output is not PEM")
			}
			if block.Type != "PRIVATE KEY" {
				t.Fatalf("decrypted block type = %q, want %q", block.Type, "PRIVATE KEY")
			}
			if string(block.Bytes) != string(want) {
				t.Fatal("decrypted key does not match the reference plaintext key")
			}
			if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err != nil {
				t.Fatalf("decrypted key does not parse as PKCS#8: %v", err)
			}
		})
	}
}

func TestDecryptPEMKeyWrongPassword(t *testing.T) {
	_, err := decryptPEMKey(readFixture(t, "enc-aes256-sha256.pem"), "not-the-password")
	if err == nil {
		t.Fatal("expected an error decrypting with the wrong password")
	}
	if !strings.Contains(err.Error(), "ClientKeyPassword is probably wrong") {
		t.Fatalf("error should point at the passphrase, got: %v", err)
	}
}

// TestLoadClientCertificateEncryptedPKCS8 exercises the full path used by
// BuildTLSConfig: certificate + passphrase-protected PKCS#8 key straight from
// disk, as an operator would configure it in .pando.toml.
func TestLoadClientCertificateEncryptedPKCS8(t *testing.T) {
	cert, err := loadClientCertificate(fixturePath("cert.pem"), fixturePath("enc-aes256-sha256.pem"), fixturePassword)
	if err != nil {
		t.Fatalf("loadClientCertificate: %v", err)
	}
	if len(cert.Certificate) == 0 || cert.PrivateKey == nil {
		t.Fatal("loaded certificate is incomplete")
	}
}

func TestBuildTLSConfigEncryptedClientKey(t *testing.T) {
	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  "https://mcp.example.internal/mcp",
		Auth: &config.MCPAuth{
			ClientCert:        fixturePath("cert.pem"),
			ClientKey:         fixturePath("enc-aes256-sha256.pem"),
			ClientKeyPassword: fixturePassword,
		},
	}
	cfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected exactly one client certificate, got %#v", cfg)
	}
}

func TestDecryptPKCS8UnsupportedScheme(t *testing.T) {
	// A plain (unencrypted) PKCS#8 key is not an EncryptedPrivateKeyInfo, so
	// it must fail loudly rather than be mistaken for one.
	if _, err := decryptPKCS8PrivateKey(plainKeyDER(t), fixturePassword); err == nil {
		t.Fatal("expected an error decrypting a non-encrypted PKCS#8 key")
	}
}

func TestBuildTLSConfigVersionAndServerName(t *testing.T) {
	tests := []struct {
		name    string
		auth    config.MCPAuth
		wantErr bool
		check   func(t *testing.T, cfg *tls.Config)
	}{
		{
			name: "server name override",
			auth: config.MCPAuth{TLSServerName: "mcp.internal"},
			check: func(t *testing.T, cfg *tls.Config) {
				if cfg.ServerName != "mcp.internal" {
					t.Fatalf("ServerName = %q", cfg.ServerName)
				}
			},
		},
		{
			name: "version range",
			auth: config.MCPAuth{MinTLSVersion: "1.2", MaxTLSVersion: "TLS1.3"},
			check: func(t *testing.T, cfg *tls.Config) {
				if cfg.MinVersion != tls.VersionTLS12 || cfg.MaxVersion != tls.VersionTLS13 {
					t.Fatalf("version range = %x..%x", cfg.MinVersion, cfg.MaxVersion)
				}
			},
		},
		{
			name:    "unsupported version",
			auth:    config.MCPAuth{MinTLSVersion: "1.1"},
			wantErr: true,
		},
		{
			name:    "inverted range",
			auth:    config.MCPAuth{MinTLSVersion: "1.3", MaxTLSVersion: "1.2"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := tc.auth
			cfg, err := BuildTLSConfig(config.MCPServer{Type: config.MCPStreamableHTTP, Auth: &auth})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("BuildTLSConfig: %v", err)
			}
			if cfg == nil {
				t.Fatal("expected a TLS config")
			}
			tc.check(t, cfg)
		})
	}
}

// TestLoadCACertPoolSystemRootsMerged pins the default behaviour: the
// configured CA is added to the system roots, not substituted for them,
// unless CACertExclusive is set.
func TestLoadCACertPoolSystemRootsMerged(t *testing.T) {
	system, err := x509.SystemCertPool()
	if err != nil || system == nil {
		t.Skip("no system certificate pool available on this host")
	}
	merged, err := loadCACertPool(fixturePath("cert.pem"), false)
	if err != nil {
		t.Fatalf("loadCACertPool(merged): %v", err)
	}
	if merged.Equal(system) {
		t.Fatal("merged pool should contain the configured CA on top of the system roots")
	}

	exclusive, err := loadCACertPool(fixturePath("cert.pem"), true)
	if err != nil {
		t.Fatalf("loadCACertPool(exclusive): %v", err)
	}
	// The exclusive pool holds exactly the configured CA, so a pool built
	// from that CA alone must be identical to it, while the merged pool
	// (system roots + CA) must not be.
	onlyCA := x509.NewCertPool()
	if !onlyCA.AppendCertsFromPEM(readFixture(t, "cert.pem")) {
		t.Fatal("failed to build the reference CA-only pool")
	}
	if !exclusive.Equal(onlyCA) {
		t.Fatal("exclusive pool should trust only the configured CA")
	}
	if merged.Equal(onlyCA) {
		t.Fatal("merged pool should not be limited to the configured CA")
	}
}

// generateNamedServerCert creates a self-signed server certificate valid ONLY
// for dnsName (no IP SANs), so a client dialing 127.0.0.1 can only verify it
// by setting TLSServerName.
func generateNamedServerCert(t *testing.T, dir, dnsName string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(42),
		Subject:               pkix.Name{CommonName: dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{dnsName},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPath = filepath.Join(dir, "named-server-cert.pem")
	keyPath = filepath.Join(dir, "named-server-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

// TestEnterpriseMTLSEndToEnd exercises the enterprise-shaped configuration as
// a whole against a live TLS server: an internal CA, a hostname that does not
// match the dial address, a passphrase-protected PKCS#8 client key, and a
// pinned TLS version — all at once, with certificate verification left ON.
func TestEnterpriseMTLSEndToEnd(t *testing.T) {
	dir := t.TempDir()
	const serverName = "mcp.internal.example"

	serverCertPath, serverKeyPath := generateNamedServerCert(t, dir, serverName)
	serverCert, err := tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
	if err != nil {
		t.Fatalf("load server cert: %v", err)
	}

	// The fixture client certificate acts as its own CA for client auth.
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(readFixture(t, "cert.pem")) {
		t.Fatal("failed to seed the client CA pool")
	}

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		MinVersion:   tls.VersionTLS13,
	}
	ts.StartTLS()
	defer ts.Close()

	srv := config.MCPServer{
		Type: config.MCPStreamableHTTP,
		URL:  ts.URL,
		Auth: &config.MCPAuth{
			ClientCert:        fixturePath("cert.pem"),
			ClientKey:         fixturePath("enc-aes256-sha256.pem"),
			ClientKeyPassword: fixturePassword,
			CACert:            serverCertPath,
			TLSServerName:     serverName,
			MinTLSVersion:     "1.3",
		},
	}
	tlsCfg, err := BuildTLSConfig(srv)
	if err != nil {
		t.Fatalf("BuildTLSConfig: %v", err)
	}
	client := HTTPClientForTLS(tlsCfg, 5*time.Second)

	resp, err := client.Get(ts.URL)
	if err != nil {
		t.Fatalf("mTLS request should succeed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if resp.TLS == nil || resp.TLS.Version != tls.VersionTLS13 {
		t.Fatalf("expected TLS 1.3, got %#v", resp.TLS)
	}

	// Same config without the name override: verification must fail, proving
	// TLSServerName is what makes the connection valid rather than some
	// accidental leniency.
	noName := *srv.Auth
	noName.TLSServerName = ""
	badCfg, err := BuildTLSConfig(config.MCPServer{Type: config.MCPStreamableHTTP, URL: ts.URL, Auth: &noName})
	if err != nil {
		t.Fatalf("BuildTLSConfig (no server name): %v", err)
	}
	if _, err := HTTPClientForTLS(badCfg, 5*time.Second).Get(ts.URL); err == nil {
		t.Fatal("expected certificate verification to fail without TLSServerName")
	}
}
