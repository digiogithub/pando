// Package tlsutil generates and manages TLS certificates for Pando's HTTP server.
//
// A local certificate authority is created once in the user's profile and
// signs the short-lived server certificate Pando serves. The CA is what a user
// imports as trusted: it stays the same across projects, restarts and server
// certificate renewals, so the import is needed only once.
package tlsutil

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
	certFile   = "server.crt"
	keyFile    = "server.key"
	lockDir    = ".lock"

	// The CA is long-lived so the trust import survives; the server
	// certificate stays under the 398 days browsers accept for a leaf.
	caValidity   = 10 * 365 * 24 * time.Hour
	leafValidity = 397 * 24 * time.Hour
	// Renew when less than this remains.
	renewBefore = 30 * 24 * time.Hour

	// maxLeafIPs bounds the addresses carried over between renewals.
	maxLeafIPs = 64

	lockTimeout = 10 * time.Second
	lockStale   = 60 * time.Second

	// LoopbackServerName is the DNS name the generated certificate always
	// carries, so callers can pin it while connecting to 127.0.0.1.
	LoopbackServerName = "localhost"
)

// permittedCIDRs are the address ranges the local CA may issue for. The CA is
// name constrained to loopback, private and link-local addresses so that a
// leaked CA key cannot be used to impersonate public sites.
var permittedCIDRs = []string{
	"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
	"169.254.0.0/16", "100.64.0.0/10",
	"::1/128", "fc00::/7", "fe80::/10",
}

// permittedDNSDomains are the DNS names the local CA may issue for.
var permittedDNSDomains = []string{LoopbackServerName, "local"}

// CertPaths holds the file paths for the TLS certificate and private key.
type CertPaths struct {
	// CertFile holds the server certificate followed by the CA certificate.
	CertFile string
	KeyFile  string
	// CAFile is the local CA certificate, the file to import as trusted. It
	// is empty for a user-provided certificate.
	CAFile string
}

// EnsureCert returns paths to a TLS cert/key pair inside dir, normally the
// user's profile directory.
//
// The local CA is created once and reused until it nears expiry. The server
// certificate is reused while it is valid, signed by that CA and covers the
// host's current local addresses; otherwise it is reissued by the same CA,
// which does not affect a trust import of the CA.
func EnsureCert(dir string) (CertPaths, error) {
	tlsDir := filepath.Join(dir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return CertPaths{}, fmt.Errorf("failed to create tls directory: %w", err)
	}

	paths := CertPaths{
		CertFile: filepath.Join(tlsDir, certFile),
		KeyFile:  filepath.Join(tlsDir, keyFile),
		CAFile:   filepath.Join(tlsDir, caCertFile),
	}
	caKeyPath := filepath.Join(tlsDir, caKeyFile)

	// Several Pando processes may start at once; serialise generation so
	// they never write mismatched certificate/key halves.
	unlock, err := lockTLSDir(tlsDir)
	if err != nil {
		return CertPaths{}, err
	}
	defer unlock()

	now := time.Now()
	caCert, caKey, err := loadCA(paths.CAFile, caKeyPath, now)
	if err != nil {
		caCert, caKey, err = generateCA(paths.CAFile, caKeyPath, now)
		if err != nil {
			return CertPaths{}, fmt.Errorf("failed to generate local CA: %w", err)
		}
	}

	dnsNames, ips := localNames()
	previous := loadLeaf(paths, caCert, now)
	if previous != nil && coversNames(previous, dnsNames, ips) {
		return paths, nil
	}
	if previous != nil {
		// Keep addresses seen before so moving between networks does not
		// reissue the certificate on every start.
		ips = mergeIPs(ips, previous.IPAddresses)
	}
	if err := generateLeaf(paths, caCert, caKey, dnsNames, ips, now); err != nil {
		return CertPaths{}, fmt.Errorf("failed to generate server certificate: %w", err)
	}
	return paths, nil
}

// LoadPinnedLoopbackTLSConfig builds a TLS client config that trusts only the
// certificates stored at certPath and verifies the server against localhost
// while allowing the caller to connect to 127.0.0.1. For an auto-generated
// pair certPath holds the server certificate followed by the local CA, so a
// server certificate renewed by that CA keeps verifying.
func LoadPinnedLoopbackTLSConfig(certPath string) (*tls.Config, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read certificate: %w", err)
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("decode certificate PEM: no certificate found")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}
	if err := cert.VerifyHostname(LoopbackServerName); err != nil {
		return nil, fmt.Errorf("certificate missing %q SAN: %w", LoopbackServerName, err)
	}
	if err := cert.VerifyHostname("127.0.0.1"); err != nil {
		return nil, fmt.Errorf("certificate missing 127.0.0.1 SAN: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, fmt.Errorf("append certificate to root pool")
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    pool,
		ServerName: LoopbackServerName,
	}, nil
}

// lockTLSDir takes an inter-process lock on tlsDir and returns its release.
func lockTLSDir(tlsDir string) (func(), error) {
	lock := filepath.Join(tlsDir, lockDir)
	deadline := time.Now().Add(lockTimeout)
	for {
		err := os.Mkdir(lock, 0o700)
		if err == nil {
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("failed to lock tls directory: %w", err)
		}
		// A crashed process may have left the lock behind.
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > lockStale {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for the tls directory lock %s", lock)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func readCertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block in %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

func readECKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block in %s", path)
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// loadCA returns the stored CA when it is usable: a CA certificate that is not
// close to expiry and whose private key matches.
func loadCA(certPath, keyPath string, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cert, err := readCertificate(certPath)
	if err != nil {
		return nil, nil, err
	}
	key, err := readECKey(keyPath)
	if err != nil {
		return nil, nil, err
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !cert.IsCA || !ok || !pub.Equal(&key.PublicKey) {
		return nil, nil, errors.New("stored CA certificate and key do not match")
	}
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter.Add(-renewBefore)) {
		return nil, nil, errors.New("stored CA certificate is expired or about to expire")
	}
	return cert, key, nil
}

// loadLeaf returns the stored server certificate when it is valid, signed by
// ca and matches the stored key; otherwise nil.
func loadLeaf(paths CertPaths, ca *x509.Certificate, now time.Time) *x509.Certificate {
	if _, err := tls.LoadX509KeyPair(paths.CertFile, paths.KeyFile); err != nil {
		return nil
	}
	leaf, err := readCertificate(paths.CertFile)
	if err != nil || leaf.IsCA {
		return nil
	}
	if leaf.CheckSignatureFrom(ca) != nil {
		return nil
	}
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter.Add(-renewBefore)) {
		return nil
	}
	return leaf
}

func coversNames(leaf *x509.Certificate, dnsNames []string, ips []net.IP) bool {
	for _, name := range dnsNames {
		if leaf.VerifyHostname(name) != nil {
			return false
		}
	}
	for _, ip := range ips {
		if leaf.VerifyHostname(ip.String()) != nil {
			return false
		}
	}
	return true
}

func mergeIPs(current, previous []net.IP) []net.IP {
	seen := map[string]bool{}
	var out []net.IP
	for _, ip := range append(append([]net.IP{}, current...), previous...) {
		if len(out) >= maxLeafIPs && len(out) >= len(current) {
			break
		}
		if s := ip.String(); !seen[s] {
			seen[s] = true
			out = append(out, ip)
		}
	}
	return out
}

func permittedIPRanges() []*net.IPNet {
	ranges := make([]*net.IPNet, 0, len(permittedCIDRs))
	for _, cidr := range permittedCIDRs {
		if _, ipNet, err := net.ParseCIDR(cidr); err == nil {
			ranges = append(ranges, ipNet)
		}
	}
	return ranges
}

// localNames returns the DNS names and IP addresses the server certificate
// should cover, restricted to what the CA's name constraints permit.
func localNames() ([]string, []net.IP) {
	dnsNames := []string{LoopbackServerName}
	if host, err := os.Hostname(); err == nil {
		host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".local"))
		if host != "" && host != LoopbackServerName && !strings.ContainsAny(host, ". _") {
			dnsNames = append(dnsNames, host+".local")
		}
	}

	ranges := permittedIPRanges()
	var ips []net.IP
	for _, ip := range collectLocalIPs() {
		for _, r := range ranges {
			if r.Contains(ip) {
				ips = append(ips, ip)
				break
			}
		}
	}
	return dnsNames, ips
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

// writeFileAtomic replaces path so a concurrent reader never sees a partial file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return fmt.Errorf("failed to write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}

func encodeCert(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}

// generateCA creates the local ECDSA P-256 certificate authority.
func generateCA(certPath, keyPath string, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	commonName := "Pando Local CA"
	if host, err := os.Hostname(); err == nil && host != "" {
		commonName += " (" + host + ")"
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Pando"},
		},
		NotBefore:                   now.Add(-time.Hour),
		NotAfter:                    now.Add(caValidity),
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLenZero:              true,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         permittedDNSDomains,
		PermittedIPRanges:           permittedIPRanges(),
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(keyPath, keyPEM, 0o600); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(certPath, encodeCert(der), 0o644); err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

// generateLeaf issues the server certificate for dnsNames and ips, signed by ca.
func generateLeaf(paths CertPaths, ca *x509.Certificate, caKey *ecdsa.PrivateKey, dnsNames []string, ips []net.IP, now time.Time) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	notAfter := now.Add(leafValidity)
	if notAfter.After(ca.NotAfter) {
		notAfter = ca.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "pando",
			Organization: []string{"Pando"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(paths.KeyFile, keyPEM, 0o600); err != nil {
		return err
	}
	chain := bytes.Join([][]byte{encodeCert(der), encodeCert(ca.Raw)}, nil)
	return writeFileAtomic(paths.CertFile, chain, 0o644)
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
