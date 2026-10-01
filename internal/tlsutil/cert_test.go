package tlsutil

import "testing"

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
