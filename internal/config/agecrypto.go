package config

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"
	"github.com/digiogithub/pando/internal/llm/models"
)

const encryptedValuePrefix = "age1:"
const ageKeysDirName = "keys"
const agePublicKeyFileName = "config.age.pub"
const agePrivateKeyFileName = "config.age.txt"

type ageKeyManager struct {
	identity  *age.X25519Identity
	recipient age.Recipient
}

func pandoConfigHome() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", appName), nil
}

func pandoAgeKeyPaths() (dir, publicKeyPath, privateKeyPath string, err error) {
	baseDir, err := pandoConfigHome()
	if err != nil {
		return "", "", "", err
	}
	dir = filepath.Join(baseDir, ageKeysDirName)
	return dir, filepath.Join(dir, agePublicKeyFileName), filepath.Join(dir, agePrivateKeyFileName), nil
}

func loadOrCreateAgeKeyManager() (*ageKeyManager, error) {
	keyDir, publicKeyPath, privateKeyPath, err := pandoAgeKeyPaths()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create age key directory: %w", err)
	}
	identity, err := loadAgeIdentity(privateKeyPath)
	if err != nil {
		return nil, err
	}
	if identity == nil {
		identity, err = age.GenerateX25519Identity()
		if err != nil {
			return nil, fmt.Errorf("failed to generate age identity: %w", err)
		}
		if err := os.WriteFile(privateKeyPath, []byte(identity.String()+"\n"), 0o600); err != nil {
			return nil, fmt.Errorf("failed to write age private key: %w", err)
		}
	}
	if err := os.WriteFile(publicKeyPath, []byte(identity.Recipient().String()+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write age public key: %w", err)
	}
	return &ageKeyManager{identity: identity, recipient: identity.Recipient()}, nil
}

func loadAgeIdentity(path string) (*age.X25519Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read age private key: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		identity, err := age.ParseX25519Identity(line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse age private key: %w", err)
		}
		return identity, nil
	}
	return nil, fmt.Errorf("age private key file %s does not contain a valid identity", path)
}

func encryptSecretString(value string) (string, error) {
	if strings.TrimSpace(value) == "" || strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	km, err := loadOrCreateAgeKeyManager()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, km.recipient)
	if err != nil {
		return "", fmt.Errorf("failed to initialize age encryption: %w", err)
	}
	if _, err := io.WriteString(w, value); err != nil {
		return "", fmt.Errorf("failed to encrypt value: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("failed to finalize age encryption: %w", err)
	}
	return encryptedValuePrefix + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func decryptSecretString(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil
	}
	payload := strings.TrimPrefix(value, encryptedValuePrefix)
	ciphertext, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("failed to decode encrypted value: %w", err)
	}
	km, err := loadOrCreateAgeKeyManager()
	if err != nil {
		return "", err
	}
	r, err := age.Decrypt(bytes.NewReader(ciphertext), km.identity)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt value: %w", err)
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read decrypted value: %w", err)
	}
	return string(plaintext), nil
}

func encryptSensitiveConfigFields(in *Config) (*Config, error) {
	if in == nil {
		return nil, nil
	}
	out := *in
	out.MCPServers = make(map[string]MCPServer, len(in.MCPServers))
	for name, server := range in.MCPServers {
		cloned := server
		if len(server.Env) > 0 {
			cloned.Env = make([]string, len(server.Env))
			for i, entry := range server.Env {
				parts := strings.SplitN(entry, "=", 2)
				if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
					cloned.Env[i] = entry
					continue
				}
				encrypted, err := encryptSecretString(parts[1])
				if err != nil {
					return nil, fmt.Errorf("encrypt MCP env %s[%d]: %w", name, i, err)
				}
				cloned.Env[i] = parts[0] + "=" + encrypted
			}
		}
		if len(server.Headers) > 0 {
			cloned.Headers = make(map[string]string, len(server.Headers))
			for key, value := range server.Headers {
				encrypted, err := encryptSecretString(value)
				if err != nil {
					return nil, fmt.Errorf("encrypt MCP header %s.%s: %w", name, key, err)
				}
				cloned.Headers[key] = encrypted
			}
		}
		out.MCPServers[name] = cloned
	}
	out.InternalTools = in.InternalTools
	var err error
	out.InternalTools.GoogleAPIKey, err = encryptSecretString(in.InternalTools.GoogleAPIKey)
	if err != nil { return nil, err }
	out.InternalTools.GoogleSearchEngineID, err = encryptSecretString(in.InternalTools.GoogleSearchEngineID)
	if err != nil { return nil, err }
	out.InternalTools.BraveAPIKey, err = encryptSecretString(in.InternalTools.BraveAPIKey)
	if err != nil { return nil, err }
	out.InternalTools.PerplexityAPIKey, err = encryptSecretString(in.InternalTools.PerplexityAPIKey)
	if err != nil { return nil, err }
	out.InternalTools.ExaAPIKey, err = encryptSecretString(in.InternalTools.ExaAPIKey)
	if err != nil { return nil, err }
	// Encrypt provider API keys (legacy map)
	if len(in.Providers) > 0 {
		out.Providers = make(map[models.ModelProvider]Provider, len(in.Providers))
		for name, p := range in.Providers {
			cloned := p
			cloned.APIKey, err = encryptSecretString(p.APIKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt provider %s APIKey: %w", name, err)
			}
			out.Providers[name] = cloned
		}
	}
	// Encrypt providerAccounts API keys (new format)
	if len(in.ProviderAccounts) > 0 {
		out.ProviderAccounts = make([]ProviderAccount, len(in.ProviderAccounts))
		copy(out.ProviderAccounts, in.ProviderAccounts)
		for i, acc := range in.ProviderAccounts {
			out.ProviderAccounts[i].APIKey, err = encryptSecretString(acc.APIKey)
			if err != nil {
				return nil, fmt.Errorf("encrypt providerAccount %s APIKey: %w", acc.ID, err)
			}
		}
	}
	// Encrypt remembrances embedding API keys
	out.Remembrances.DocumentEmbeddingAPIKey, err = encryptSecretString(in.Remembrances.DocumentEmbeddingAPIKey)
	if err != nil { return nil, err }
	out.Remembrances.CodeEmbeddingAPIKey, err = encryptSecretString(in.Remembrances.CodeEmbeddingAPIKey)
	if err != nil { return nil, err }
	return &out, nil
}

func decryptSensitiveConfigFields(in *Config) error {
	if in == nil {
		return nil
	}
	var err error
	in.InternalTools.GoogleAPIKey, err = decryptSecretString(in.InternalTools.GoogleAPIKey)
	if err != nil { return err }
	in.InternalTools.GoogleSearchEngineID, err = decryptSecretString(in.InternalTools.GoogleSearchEngineID)
	if err != nil { return err }
	in.InternalTools.BraveAPIKey, err = decryptSecretString(in.InternalTools.BraveAPIKey)
	if err != nil { return err }
	in.InternalTools.PerplexityAPIKey, err = decryptSecretString(in.InternalTools.PerplexityAPIKey)
	if err != nil { return err }
	in.InternalTools.ExaAPIKey, err = decryptSecretString(in.InternalTools.ExaAPIKey)
	if err != nil { return err }
	// Decrypt provider API keys (legacy map)
	for name, p := range in.Providers {
		decrypted, err := decryptSecretString(p.APIKey)
		if err != nil {
			return fmt.Errorf("decrypt provider %s APIKey: %w", name, err)
		}
		p.APIKey = decrypted
		in.Providers[name] = p
	}
	// Decrypt providerAccounts API keys (new format)
	for i, acc := range in.ProviderAccounts {
		decrypted, err := decryptSecretString(acc.APIKey)
		if err != nil {
			return fmt.Errorf("decrypt providerAccount %s APIKey: %w", acc.ID, err)
		}
		in.ProviderAccounts[i].APIKey = decrypted
	}
	// Decrypt remembrances embedding API keys
	in.Remembrances.DocumentEmbeddingAPIKey, err = decryptSecretString(in.Remembrances.DocumentEmbeddingAPIKey)
	if err != nil { return err }
	in.Remembrances.CodeEmbeddingAPIKey, err = decryptSecretString(in.Remembrances.CodeEmbeddingAPIKey)
	if err != nil { return err }
	for name, server := range in.MCPServers {
		updated := server
		if len(server.Env) > 0 {
			updated.Env = make([]string, len(server.Env))
			for i, entry := range server.Env {
				parts := strings.SplitN(entry, "=", 2)
				if len(parts) != 2 {
					updated.Env[i] = entry
					continue
				}
				decrypted, err := decryptSecretString(parts[1])
				if err != nil {
					return fmt.Errorf("decrypt MCP env %s[%d]: %w", name, i, err)
				}
				updated.Env[i] = parts[0] + "=" + decrypted
			}
		}
		if len(server.Headers) > 0 {
			updated.Headers = cloneStringMap(server.Headers)
			for key, value := range updated.Headers {
				decrypted, err := decryptSecretString(value)
				if err != nil {
					return fmt.Errorf("decrypt MCP header %s.%s: %w", name, key, err)
				}
				updated.Headers[key] = decrypted
			}
		}
		in.MCPServers[name] = updated
	}
	return nil
}
