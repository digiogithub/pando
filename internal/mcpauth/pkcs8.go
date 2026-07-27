package mcpauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des"
	"crypto/pbkdf2"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509/pkix"
	"encoding/asn1"
	"fmt"
	"hash"

	"golang.org/x/crypto/scrypt"
)

// This file implements decryption of PKCS#8 "ENCRYPTED PRIVATE KEY" PEM
// blocks protected with PBES2 (RFC 8018 §6.2) — the format `openssl genpkey`,
// `openssl pkcs8 -topk8` and most modern enterprise PKI tooling produce by
// default when a passphrase is supplied.
//
// Go's standard library deliberately does not implement this (crypto/x509
// only handles the legacy RFC 1423 PEM encryption via the deprecated
// DecryptPEMBlock), so without this file an operator whose corporate CA
// handed them a passphrase-protected client key would have to decrypt it
// out-of-band before Pando could use it for mTLS.
//
// Supported: PBES2 with PBKDF2 (HMAC-SHA1/224/256/384/512) or scrypt key
// derivation, and AES-128/192/256-CBC or DES-EDE3-CBC encryption. Anything
// else (notably the obsolete PBES1 / PKCS#12 derivations) returns a named
// error identifying the algorithm rather than a generic failure.

var (
	oidPBES2  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	oidScrypt = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11591, 4, 11}

	oidHMACWithSHA1   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHMACWithSHA224 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 8}
	oidHMACWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidHMACWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	oidHMACWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}

	oidAES128CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidDESEDE3   = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
)

// encryptedPrivateKeyInfo is RFC 5208 §6 EncryptedPrivateKeyInfo.
type encryptedPrivateKeyInfo struct {
	Algo          pkix.AlgorithmIdentifier
	EncryptedData []byte
}

// pbes2Params is RFC 8018 appendix A.4 PBES2-params.
type pbes2Params struct {
	KDF              pkix.AlgorithmIdentifier
	EncryptionScheme pkix.AlgorithmIdentifier
}

// pbkdf2Params is RFC 8018 appendix A.2 PBKDF2-params. Only the
// "specified" (OCTET STRING) salt choice is accepted; the PRF defaults to
// HMAC-SHA1 when absent, per the ASN.1 DEFAULT.
type pbkdf2Params struct {
	Salt       []byte
	Iterations int
	KeyLength  int                      `asn1:"optional"`
	PRF        pkix.AlgorithmIdentifier `asn1:"optional"`
}

// scryptParams is RFC 7914 §7 scrypt-params.
type scryptParams struct {
	Salt      []byte
	CostN     int
	BlockSize int
	Parallel  int
	KeyLength int `asn1:"optional"`
}

// decryptPKCS8PrivateKey decrypts a PBES2-protected PKCS#8 private key,
// returning the unencrypted PKCS#8 DER so the caller can re-wrap it in a
// plain "PRIVATE KEY" PEM block.
func decryptPKCS8PrivateKey(der []byte, password string) ([]byte, error) {
	var info encryptedPrivateKeyInfo
	if _, err := asn1.Unmarshal(der, &info); err != nil {
		return nil, fmt.Errorf("parse encrypted PKCS#8 private key: %w", err)
	}
	if !info.Algo.Algorithm.Equal(oidPBES2) {
		return nil, fmt.Errorf("encrypted PKCS#8 private key uses unsupported scheme %v (only PBES2 is supported; re-encrypt with `openssl pkcs8 -topk8 -v2 aes-256-cbc`)", info.Algo.Algorithm)
	}

	var params pbes2Params
	if _, err := asn1.Unmarshal(info.Algo.Parameters.FullBytes, &params); err != nil {
		return nil, fmt.Errorf("parse PBES2 parameters: %w", err)
	}

	block, iv, err := pbes2Cipher(params.EncryptionScheme)
	if err != nil {
		return nil, err
	}
	key, err := pbes2DeriveKey(params.KDF, password, block.keyLen)
	if err != nil {
		return nil, err
	}
	blk, err := block.new(key)
	if err != nil {
		return nil, fmt.Errorf("initialize %s cipher: %w", block.name, err)
	}
	if len(iv) != blk.BlockSize() {
		return nil, fmt.Errorf("encrypted PKCS#8 private key has a %d-byte IV, expected %d for %s", len(iv), blk.BlockSize(), block.name)
	}
	if len(info.EncryptedData) == 0 || len(info.EncryptedData)%blk.BlockSize() != 0 {
		return nil, fmt.Errorf("encrypted PKCS#8 private key payload is not a whole number of %s blocks", block.name)
	}

	plaintext := make([]byte, len(info.EncryptedData))
	cipher.NewCBCDecrypter(blk, iv).CryptBlocks(plaintext, info.EncryptedData)

	unpadded, err := stripPKCS7Padding(plaintext, blk.BlockSize())
	if err != nil {
		// A bad passphrase almost always surfaces here as garbage padding, so
		// name that likely cause instead of leaking the padding detail.
		return nil, fmt.Errorf("could not decrypt the client key: the ClientKeyPassword is probably wrong")
	}
	return unpadded, nil
}

// pbes2CipherSpec describes one supported PBES2 encryption scheme.
type pbes2CipherSpec struct {
	name   string
	keyLen int
	new    func(key []byte) (cipher.Block, error)
}

// pbes2Cipher maps the encryption-scheme AlgorithmIdentifier to a cipher
// spec and the IV carried in its parameters.
func pbes2Cipher(scheme pkix.AlgorithmIdentifier) (pbes2CipherSpec, []byte, error) {
	var spec pbes2CipherSpec
	switch {
	case scheme.Algorithm.Equal(oidAES128CBC):
		spec = pbes2CipherSpec{name: "AES-128-CBC", keyLen: 16, new: aes.NewCipher}
	case scheme.Algorithm.Equal(oidAES192CBC):
		spec = pbes2CipherSpec{name: "AES-192-CBC", keyLen: 24, new: aes.NewCipher}
	case scheme.Algorithm.Equal(oidAES256CBC):
		spec = pbes2CipherSpec{name: "AES-256-CBC", keyLen: 32, new: aes.NewCipher}
	case scheme.Algorithm.Equal(oidDESEDE3):
		spec = pbes2CipherSpec{name: "DES-EDE3-CBC", keyLen: 24, new: des.NewTripleDESCipher}
	default:
		return pbes2CipherSpec{}, nil, fmt.Errorf("encrypted PKCS#8 private key uses unsupported cipher %v (supported: AES-128/192/256-CBC, DES-EDE3-CBC)", scheme.Algorithm)
	}

	var iv []byte
	if _, err := asn1.Unmarshal(scheme.Parameters.FullBytes, &iv); err != nil {
		return pbes2CipherSpec{}, nil, fmt.Errorf("parse %s IV: %w", spec.name, err)
	}
	return spec, iv, nil
}

// pbes2DeriveKey runs the key-derivation function named by kdf over password,
// producing a key of keyLen bytes (the KDF's own declared KeyLength is
// honored when present and consistent).
func pbes2DeriveKey(kdf pkix.AlgorithmIdentifier, password string, keyLen int) ([]byte, error) {
	switch {
	case kdf.Algorithm.Equal(oidPBKDF2):
		var params pbkdf2Params
		if _, err := asn1.Unmarshal(kdf.Parameters.FullBytes, &params); err != nil {
			return nil, fmt.Errorf("parse PBKDF2 parameters: %w", err)
		}
		if params.Iterations <= 0 {
			return nil, fmt.Errorf("PBKDF2 iteration count %d is invalid", params.Iterations)
		}
		if params.KeyLength > 0 {
			keyLen = params.KeyLength
		}
		h, err := pbkdf2PRF(params.PRF)
		if err != nil {
			return nil, err
		}
		return pbkdf2.Key(h, password, params.Salt, params.Iterations, keyLen)
	case kdf.Algorithm.Equal(oidScrypt):
		var params scryptParams
		if _, err := asn1.Unmarshal(kdf.Parameters.FullBytes, &params); err != nil {
			return nil, fmt.Errorf("parse scrypt parameters: %w", err)
		}
		if params.KeyLength > 0 {
			keyLen = params.KeyLength
		}
		return scrypt.Key([]byte(password), params.Salt, params.CostN, params.BlockSize, params.Parallel, keyLen)
	default:
		return nil, fmt.Errorf("encrypted PKCS#8 private key uses unsupported key-derivation function %v (supported: PBKDF2, scrypt)", kdf.Algorithm)
	}
}

// pbkdf2PRF maps the PBKDF2 pseudo-random-function AlgorithmIdentifier to a
// hash constructor. A zero-value identifier means the field was absent,
// which per RFC 8018 appendix A.2 defaults to HMAC-SHA1.
func pbkdf2PRF(prf pkix.AlgorithmIdentifier) (func() hash.Hash, error) {
	switch {
	case prf.Algorithm == nil, prf.Algorithm.Equal(oidHMACWithSHA1):
		return sha1.New, nil
	case prf.Algorithm.Equal(oidHMACWithSHA224):
		return sha256.New224, nil
	case prf.Algorithm.Equal(oidHMACWithSHA256):
		return sha256.New, nil
	case prf.Algorithm.Equal(oidHMACWithSHA384):
		return sha512.New384, nil
	case prf.Algorithm.Equal(oidHMACWithSHA512):
		return sha512.New, nil
	default:
		return nil, fmt.Errorf("encrypted PKCS#8 private key uses unsupported PBKDF2 PRF %v", prf.Algorithm)
	}
}

// stripPKCS7Padding removes and validates the PKCS#7 padding CBC-mode PBES2
// always applies.
func stripPKCS7Padding(b []byte, blockSize int) ([]byte, error) {
	if len(b) == 0 || len(b)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded length %d", len(b))
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > blockSize || pad > len(b) {
		return nil, fmt.Errorf("invalid padding byte %d", pad)
	}
	for _, c := range b[len(b)-pad:] {
		if int(c) != pad {
			return nil, fmt.Errorf("inconsistent padding")
		}
	}
	return b[:len(b)-pad], nil
}
