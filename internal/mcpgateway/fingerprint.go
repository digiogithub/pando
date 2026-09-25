package mcpgateway

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/digiogithub/pando/internal/config"
)

// Cached gateway discovery (PANDO-US-0068).
//
// Every Pando process start used to reconnect to every configured MCP server
// just to list its tools, even though the catalog is persisted in SQLite. For
// Xcode's mcpbridge each new connection raises an "Allow ... to access Xcode?"
// prompt, and Xcode relaunches `pando acp` every few minutes. The gateway now
// records a fingerprint of each server's raw configuration next to its catalog
// and skips discovery when both are present and the fingerprint is unchanged.

// fingerprintInput is the subset of config.MCPServer that decides which server
// is reached and what it advertises. Timeout and sandbox flags are left out on
// purpose: they change how Pando talks to the server, not its tool list.
type fingerprintInput struct {
	Type    config.MCPType    `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     []string          `json:"env"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Auth    *config.MCPAuth   `json:"auth,omitempty"`
}

// ConfigFingerprint returns a stable hash of the server's RAW configuration.
// Callers must pass the configuration before config.ResolveMCPServerSecrets:
// hashing resolved values would put secret material into the digest input and
// make the fingerprint change whenever a secret store rotates a token.
func ConfigFingerprint(srv config.MCPServer) string {
	payload, err := json.Marshal(fingerprintInput{
		Type:    srv.Type,
		Command: srv.Command,
		Args:    srv.Args,
		Env:     srv.Env,
		URL:     srv.URL,
		Headers: srv.Headers, // json.Marshal sorts map keys: stable output.
		Auth:    srv.Auth,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// serverFingerprint returns the stored fingerprint for a server, or "" when
// none is stored (or the table is unavailable, which disables the cache
// instead of failing discovery).
func (r *Registry) serverFingerprint(ctx context.Context, serverName string) (string, error) {
	var fp string
	err := r.db.QueryRowContext(ctx,
		`SELECT fingerprint FROM mcp_server_fingerprints WHERE server_name = ?`, serverName,
	).Scan(&fp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return fp, err
}

// setServerFingerprint records the fingerprint of the configuration a
// server's catalog was discovered with.
func (r *Registry) setServerFingerprint(ctx context.Context, serverName, fingerprint string) error {
	if fingerprint == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO mcp_server_fingerprints (server_name, fingerprint, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(server_name) DO UPDATE SET
			fingerprint = excluded.fingerprint,
			updated_at = excluded.updated_at
	`, serverName, fingerprint, time.Now().UTC())
	return err
}

// deleteServerFingerprint forgets a server's fingerprint.
func (r *Registry) deleteServerFingerprint(ctx context.Context, serverName string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM mcp_server_fingerprints WHERE server_name = ?`, serverName)
	return err
}

// serverToolCount returns how many catalog rows a server has.
func (r *Registry) serverToolCount(ctx context.Context, serverName string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM mcp_tool_registry WHERE server_name = ?`, serverName,
	).Scan(&n)
	return n, err
}

// hasCachedCatalog reports whether the server's stored catalog can be reused:
// it has at least one tool row and was discovered with the same configuration.
func (r *Registry) hasCachedCatalog(ctx context.Context, serverName, fingerprint string) bool {
	if fingerprint == "" {
		return false
	}
	stored, err := r.serverFingerprint(ctx, serverName)
	if err != nil || stored != fingerprint {
		return false
	}
	n, err := r.serverToolCount(ctx, serverName)
	return err == nil && n > 0
}
