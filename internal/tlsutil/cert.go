// Package tlsutil generates and manages TLS certificates for Pando's HTTP server.
// It creates a self-signed certificate stored in the user's data directory so that
// the PWA installation prompt is available when accessing Pando remotely.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	certFile = "server.crt"
	keyFile  = "server.key"
	// Certificate validity: 10 years.
	certValidity = 10 * 365 * 24 * time.Hour
)

// CertPaths holds the file paths for the TLS certificate and private key.
type CertPaths struct {
	CertFile string
	KeyFile  string
}

// EnsureCert returns paths to a TLS cert/key pair inside dataDir.
// If the files already exist they are reused; otherwise a new self-signed
// ECDSA P-256 certificate is generated and persisted.
func EnsureCert(dataDir string) (CertPaths, error) {
	tlsDir := filepath.Join(dataDir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return CertPaths{}, fmt.Errorf("failed to create tls directory: %w", err)
	}

	paths := CertPaths{
		CertFile: filepath.Join(tlsDir, certFile),
		KeyFile:  filepath.Join(tlsDir, keyFile),
	}

	// Reuse existing certificate if both files are present and not expired.
	if _, err := os.Stat(paths.CertFile); err == nil {
		if _, err := os.Stat(paths.KeyFile); err == nil {
			if valid, _ := isCertValid(paths.CertFile); valid {
				return paths, nil
			}
		}
	}

	// Generate a new certificate.
	if err := generateSelfSigned(paths); err != nil {
		return CertPaths{}, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	return paths, nil
}

// isCertValid reads the PEM certificate and checks that it has not expired.
func isCertValid(certPath string) (bool, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false, fmt.Errorf("failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}
	// Renew if less than 30 days remain.
	return time.Now().Before(cert.NotAfter.Add(-30 * 24 * time.Hour)), nil
}

// generateSelfSigned creates a new ECDSA P-256 self-signed certificate.
// The certificate includes SANs for localhost and all local interface IPs.
func generateSelfSigned(paths CertPaths) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "pando",
			Organization: []string{"Pando"},
		},
		NotBefore:             now,
		NotAfter:              now.Add(certValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           collectLocalIPs(),
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate file.
	certOut, err := os.OpenFile(paths.CertFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", paths.CertFile, err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write certificate PEM: %w", err)
	}

	// Write private key file.
	keyOut, err := os.OpenFile(paths.KeyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open %s for writing: %w", paths.KeyFile, err)
	}
	defer keyOut.Close()
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}); err != nil {
		return fmt.Errorf("failed to write key PEM: %w", err)
	}

	return nil
}

// collectLocalIPs returns all non-loopback and loopback unicast IPs on the host.
func collectLocalIPs() []net.IP {
	ips := []net.IP{
		net.ParseIP("127.0.0.1"),
		net.ParseIP("::1"),
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	seen := map[string]bool{
		"127.0.0.1": true,
		"::1":       true,
	}

	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			s := ip.String()
			if !seen[s] {
				seen[s] = true
				ips = append(ips, ip)
			}
		}
	}

	return ips
}
