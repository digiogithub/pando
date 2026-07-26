package mcpauth

import (
	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/logging"
)

// encryptEntryValues returns a copy of e with Tokens.AccessToken,
// Tokens.RefreshToken and ClientInfo.ClientSecret encrypted at rest via
// config.EncryptSecretString (the same AGE envelope .pando.toml secrets use;
// see internal/config/agecrypto.go). Every other field — server URL, expiry,
// scopes, client id, issuer, timestamps — is left untouched so the JSON
// structure stays readable and debuggable, mirroring how .pando.toml
// encrypts individual values rather than the whole document.
//
// If AGE key material cannot be loaded/generated (e.g. a read-only or
// otherwise inaccessible config home), encryption is skipped entirely for
// this entry: the values are kept in clear, a single logging.Warn is
// emitted, and ok is false. Losing at-rest encryption on a fresh/misconfigured
// machine is preferable to hard-failing and cutting the user off from every
// MCP server they had authorized.
func encryptEntryValues(name string, e Entry) (out Entry, ok bool) {
	out = e
	ok = true

	if e.Tokens != nil {
		tok := *e.Tokens
		if enc, err := config.EncryptSecretString(tok.AccessToken); err != nil {
			logging.Warn("mcpauth: failed to encrypt access token at rest, storing in clear", "server", name, "error", err)
			ok = false
		} else {
			tok.AccessToken = enc
		}
		if enc, err := config.EncryptSecretString(tok.RefreshToken); err != nil {
			logging.Warn("mcpauth: failed to encrypt refresh token at rest, storing in clear", "server", name, "error", err)
			ok = false
		} else {
			tok.RefreshToken = enc
		}
		out.Tokens = &tok
	}
	if e.ClientInfo != nil {
		ci := *e.ClientInfo
		if enc, err := config.EncryptSecretString(ci.ClientSecret); err != nil {
			logging.Warn("mcpauth: failed to encrypt client secret at rest, storing in clear", "server", name, "error", err)
			ok = false
		} else {
			ci.ClientSecret = enc
		}
		out.ClientInfo = &ci
	}
	return out, ok
}

// decryptEntryValues returns a copy of e with any AGE-encrypted
// AccessToken/RefreshToken/ClientSecret values decrypted back to plaintext
// for in-memory use. A value with no "age1:" prefix (a legacy plaintext
// file, or a field that fell back to clear-text on a machine without an AGE
// key) is returned unchanged, so this is safe to call unconditionally on
// every load regardless of when/how the entry was written.
//
// Decryption failure for one entry's Tokens or ClientInfo does not abort the
// whole store load: the offending sub-struct is dropped (treated as absent,
// so the caller just re-authorizes) and every other entry is unaffected.
func decryptEntryValues(name string, e Entry) Entry {
	out := e
	if e.Tokens != nil {
		tok := *e.Tokens
		access, accessErr := config.DecryptSecretString(tok.AccessToken)
		refresh, refreshErr := config.DecryptSecretString(tok.RefreshToken)
		if accessErr != nil || refreshErr != nil {
			logging.Warn("mcpauth: failed to decrypt stored tokens, discarding entry (re-login required)",
				"server", name, "accessError", accessErr, "refreshError", refreshErr)
			out.Tokens = nil
		} else {
			tok.AccessToken = access
			tok.RefreshToken = refresh
			out.Tokens = &tok
		}
	}
	if e.ClientInfo != nil {
		ci := *e.ClientInfo
		secret, err := config.DecryptSecretString(ci.ClientSecret)
		if err != nil {
			logging.Warn("mcpauth: failed to decrypt stored client secret, discarding client info (re-registration required)",
				"server", name, "error", err)
			out.ClientInfo = nil
		} else {
			ci.ClientSecret = secret
			out.ClientInfo = &ci
		}
	}
	return out
}
