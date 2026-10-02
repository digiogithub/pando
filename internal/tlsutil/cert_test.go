package tlsutil

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestEnsureCertGeneratesLoopbackCertificate(t *testing.T) {
	paths, err := EnsureCert(t.TempDir())
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}

	cfg, err := LoadPinnedLoopbackTLSConfig(paths.CertFile)
	if err != nil {
		t.Fatalf("LoadPinnedLoopbackTLSConfig: %v", err)
	}
	if cfg.ServerName != LoopbackServerName {
		t.Fatalf("ServerName = %q, want %q", cfg.ServerName, LoopbackServerName)
	}
	if cfg.RootCAs == nil {
		t.Fatal("expected RootCAs to be populated")
	}
}

func TestLoadPinnedLoopbackTLSConfigRejectsMissingCert(t *testing.T) {
	if _, err := LoadPinnedLoopbackTLSConfig(t.TempDir() + "/missing.crt"); err == nil {
		t.Fatal("expected an error for a missing certificate")
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestEnsureCertReusesCAAndServerCertificate(t *testing.T) {
	dir := t.TempDir()
	first, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	ca, cert := readFile(t, first.CAFile), readFile(t, first.CertFile)

	second, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	if second != first {
		t.Fatalf("paths changed: %+v != %+v", second, first)
	}
	if !bytes.Equal(ca, readFile(t, second.CAFile)) {
		t.Fatal("CA certificate was regenerated")
	}
	if !bytes.Equal(cert, readFile(t, second.CertFile)) {
		t.Fatal("server certificate was regenerated")
	}
}

func TestEnsureCertServerCertificateVerifiesAgainstCA(t *testing.T) {
	paths, err := EnsureCert(t.TempDir())
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	ca, err := readCertificate(paths.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := readCertificate(paths.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.IsCA || leaf.IsCA {
		t.Fatalf("IsCA: ca=%v leaf=%v", ca.IsCA, leaf.IsCA)
	}
	if got := leaf.NotAfter.Sub(leaf.NotBefore); got > 398*24*time.Hour {
		t.Fatalf("server certificate validity %v exceeds 398 days", got)
	}

	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, name := range []string{"localhost", "127.0.0.1"} {
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: name}); err != nil {
			t.Fatalf("verify %s against the CA: %v", name, err)
		}
	}
	if _, err := tls.LoadX509KeyPair(paths.CertFile, paths.KeyFile); err != nil {
		t.Fatalf("server key pair: %v", err)
	}
}

func TestEnsureCertReissuesServerCertificateKeepingCA(t *testing.T) {
	dir := t.TempDir()
	first, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	ca, cert := readFile(t, first.CAFile), readFile(t, first.CertFile)
	pinned, err := LoadPinnedLoopbackTLSConfig(first.CertFile)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(first.KeyFile); err != nil {
		t.Fatal(err)
	}
	second, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	if !bytes.Equal(ca, readFile(t, second.CAFile)) {
		t.Fatal("CA certificate must survive a server certificate reissue")
	}
	if bytes.Equal(cert, readFile(t, second.CertFile)) {
		t.Fatal("server certificate was not reissued")
	}

	// A client that pinned the old file still trusts the reissued certificate.
	leaf, err := readCertificate(second.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: pinned.RootCAs, DNSName: LoopbackServerName}); err != nil {
		t.Fatalf("reissued certificate rejected by the earlier pin: %v", err)
	}
}

func TestCARejectsPublicNames(t *testing.T) {
	dir := t.TempDir()
	paths, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	ca, key, err := loadCA(paths.CAFile, filepath.Join(dir, "tls", caKeyFile), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rogue := CertPaths{CertFile: filepath.Join(dir, "rogue.crt"), KeyFile: filepath.Join(dir, "rogue.key")}
	if err := generateLeaf(rogue, ca, key, []string{"example.com"}, []net.IP{net.ParseIP("8.8.8.8")}, time.Now()); err != nil {
		t.Fatal(err)
	}
	leaf, err := readCertificate(rogue.CertFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "example.com"}); err == nil {
		t.Fatal("name constraints must reject a certificate for a public name")
	}
}

func TestLoadCARejectsExpiring(t *testing.T) {
	dir := t.TempDir()
	paths, err := EnsureCert(dir)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	keyPath := filepath.Join(dir, "tls", caKeyFile)
	if _, _, err := loadCA(paths.CAFile, keyPath, time.Now().Add(caValidity-time.Hour)); err == nil {
		t.Fatal("a CA about to expire must not be reused")
	}
	ca, _, err := loadCA(paths.CAFile, keyPath, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if loadLeaf(paths, ca, time.Now().Add(leafValidity-time.Hour)) != nil {
		t.Fatal("a server certificate about to expire must not be reused")
	}
}

func TestEnsureCertConcurrent(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paths, err := EnsureCert(dir)
			if err == nil {
				_, err = tls.LoadX509KeyPair(paths.CertFile, paths.KeyFile)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
