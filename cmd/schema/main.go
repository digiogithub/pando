package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

// JSONSchemaType represents a JSON Schema type
type JSONSchemaType struct {
	Type                 string           `json:"type,omitempty"`
	Description          string           `json:"description,omitempty"`
	Properties           map[string]any   `json:"properties,omitempty"`
	Required             []string         `json:"required,omitempty"`
	AdditionalProperties any              `json:"additionalProperties,omitempty"`
	Enum                 []any            `json:"enum,omitempty"`
	Items                map[string]any   `json:"items,omitempty"`
	OneOf                []map[string]any `json:"oneOf,omitempty"`
	AnyOf                []map[string]any `json:"anyOf,omitempty"`
	Default              any              `json:"default,omitempty"`
}

func main() {
	schema := generateSchema()

	// Pretty print the schema
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding schema: %v\n", err)
		os.Exit(1)
	}
}

func generateSchema() map[string]any {
	schema := map[string]any{
		"$schema":     "http://json-schema.org/draft-07/schema#",
		"title":       "Pando Configuration",
		"description": "Configuration schema for the Pando application",
		"type":        "object",
		"properties":  map[string]any{},
	}

	// Add Data configuration
	schema["properties"].(map[string]any)["data"] = map[string]any{
		"type":        "object",
		"description": "Storage configuration",
		"properties": map[string]any{
			"directory": map[string]any{
				"type":        "string",
				"description": "Directory where application data is stored",
				"default":     ".pando",
			},
		},
		"required": []string{"directory"},
	}

	// Add working directory
	schema["properties"].(map[string]any)["wd"] = map[string]any{
		"type":        "string",
		"description": "Working directory for the application",
	}

	// Add debug flags
	schema["properties"].(map[string]any)["debug"] = map[string]any{
		"type":        "boolean",
		"description": "Enable debug mode",
		"default":     false,
	}

	schema["properties"].(map[string]any)["debugLSP"] = map[string]any{
		"type":        "boolean",
		"description": "Enable LSP debug mode",
		"default":     false,
	}

	schema["properties"].(map[string]any)["contextPaths"] = map[string]any{
		"type":        "array",
		"description": "Context paths for the application",
		"items": map[string]any{
			"type": "string",
		},
		"default": []string{
			".github/copilot-instructions.md",
			".cursorrules",
			".cursor/rules/",
			"AGENTS.md",
			"PANDO.md",
			"CLAUDE.md",
			"CLAUDE.local.md",
			"pando.md",
			"pando.local.md",
			"Pando.md",
			"Pando.local.md",
			"PANDO.local.md",
		},
	}

	schema["properties"].(map[string]any)["skills"] = map[string]any{
		"type":        "object",
		"description": "Skill discovery and prompt injection configuration",
		"properties": map[string]any{
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable skills support",
				"default":     true,
			},
			"paths": map[string]any{
				"type":        "array",
				"description": "Additional skill search paths",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
	}

	schema["properties"].(map[string]any)["tui"] = map[string]any{
		"type":        "object",
		"description": "Terminal User Interface configuration",
		"properties": map[string]any{
			"theme": map[string]any{
				"type":        "string",
				"description": "TUI theme name",
				"default":     "pando",
				"enum": []string{
					"pando",
					"catppuccin",
					"dracula",
					"flexoki",
					"gruvbox",
					"monokai",
					"onedark",
					"tokyonight",
					"tron",
				},
			},
		},
	}

	schema["properties"].(map[string]any)["permissions"] = map[string]any{
		"type":        "object",
		"description": "Runtime permissions behavior",
		"properties": map[string]any{
			"autoApproveTools": map[string]any{
				"type":        "boolean",
				"description": "Automatically approve tool-driven file changes in local interactive sessions",
				"default":     false,
			},
		},
	}

	schema["properties"].(map[string]any)["mesnada"] = map[string]any{
		"type":        "object",
		"description": "Mesnada integration configuration",
		"properties": map[string]any{
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable Mesnada integration",
				"default":     false,
			},
			"server": map[string]any{
				"type":        "object",
				"description": "Mesnada HTTP server configuration",
				"properties": map[string]any{
					"host": map[string]any{
						"type":        "string",
						"description": "Mesnada server host",
						"default":     "127.0.0.1",
					},
					"port": map[string]any{
						"type":        "integer",
						"description": "Mesnada server port",
						"default":     9767,
					},
				},
			},
			"orchestrator": map[string]any{
				"type":        "object",
				"description": "Mesnada orchestrator configuration",
				"properties": map[string]any{
					"storePath": map[string]any{
						"type":        "string",
						"description": "Path to the Mesnada task store",
					},
					"logDir": map[string]any{
						"type":        "string",
						"description": "Directory for Mesnada task logs",
					},
					"maxParallel": map[string]any{
						"type":        "integer",
						"description": "Maximum number of parallel Mesnada tasks",
						"default":     5,
					},
					"defaultEngine": map[string]any{
						"type":        "string",
						"description": "Default engine for Mesnada tasks",
						"default":     "copilot",
					},
					"defaultMcpConfig": map[string]any{
						"type":        "string",
						"description": "Default MCP config for Mesnada tasks",
					},
					"personaPath": map[string]any{
						"type":        "string",
						"description": "Path to Mesnada persona definitions",
					},
				},
			},
			"acp": map[string]any{
				"type":        "object",
				"description": "Mesnada ACP configuration",
				"properties": map[string]any{
					"enabled": map[string]any{
						"type":        "boolean",
						"description": "Enable ACP-backed Mesnada agents",
					},
					"defaultAgent": map[string]any{
						"type":        "string",
						"description": "Default ACP agent for Mesnada",
					},
					"autoPermission": map[string]any{
						"type":        "boolean",
						"description": "Automatically approve ACP permissions",
					},
				},
			},
			"tui": map[string]any{
				"type":        "object",
				"description": "Mesnada TUI configuration",
				"properties": map[string]any{
					"enabled": map[string]any{
						"type":        "boolean",
						"description": "Enable Mesnada TUI features",
						"default":     true,
					},
					"webui": map[string]any{
						"type":        "boolean",
						"description": "Enable the Mesnada web UI",
						"default":     true,
					},
				},
			},
		},
	}

	// Add MCP servers
	schema["properties"].(map[string]any)["mcpServers"] = map[string]any{
		"type":        "object",
		"description": "Model Control Protocol server configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "MCP server configuration",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Command to execute for the MCP server",
				},
				"env": map[string]any{
					"type":        "array",
					"description": "Environment variables for the MCP server",
					"items": map[string]any{
						"type": "string",
					},
				},
				"args": map[string]any{
					"type":        "array",
					"description": "Command arguments for the MCP server",
					"items": map[string]any{
						"type": "string",
					},
				},
				"type": map[string]any{
					"type":        "string",
					"description": "Type of MCP server",
					"enum":        []string{"stdio", "sse", "streamable-http"},
					"default":     "stdio",
				},
				"url": map[string]any{
					"type":        "string",
					"description": "URL for SSE and streamable-http type MCP servers",
				},
				"headers": map[string]any{
					"type":        "object",
					"description": "HTTP headers for SSE and streamable-http type MCP servers",
					"additionalProperties": map[string]any{
						"type": "string",
					},
				},
				"auth": map[string]any{
					"type":        "object",
					"description": "Authentication for SSE and streamable-http type MCP servers. Ignored by stdio servers, which take credentials from Env instead. See docs/mcp-authentication.md.",
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"description": "Authentication mechanism. \"none\" (default) sends no credentials. \"bearer\" sends 'Authorization: Bearer <Token>'. \"basic\" sends HTTP Basic auth using Username/Password. \"header\" sends Token as the raw value of a custom header named HeaderName. \"oauth\" drives the interactive OAuth 2.1 authorization-code + PKCE flow (pando mcp login). \"oauth_client_credentials\" mints a token non-interactively via the OAuth 2.1 client_credentials grant, for headless/CI use.",
							"enum":        []string{"none", "bearer", "basic", "header", "oauth", "oauth_client_credentials"},
							"default":     "none",
						},
						"token": map[string]any{
							"type":        "string",
							"description": "Bearer token or custom-header value. Required for type=bearer and type=header. Encrypted at rest with AGE.",
						},
						"username": map[string]any{
							"type":        "string",
							"description": "Username for type=basic.",
						},
						"password": map[string]any{
							"type":        "string",
							"description": "Password for type=basic. Encrypted at rest with AGE.",
						},
						"headerName": map[string]any{
							"type":        "string",
							"description": "Header name to use for type=header. Defaults to \"Authorization\" when empty.",
						},
						"oauth": map[string]any{
							"type":        "object",
							"description": "OAuth 2.1 client parameters, used by type=oauth and type=oauth_client_credentials.",
							"properties": map[string]any{
								"clientID": map[string]any{
									"type":        "string",
									"description": "Pre-registered OAuth client ID. Leave empty to let Pando perform RFC 7591 dynamic client registration (type=oauth only).",
								},
								"clientSecret": map[string]any{
									"type":        "string",
									"description": "Pre-registered OAuth client secret, when the authorization server issues one. Encrypted at rest with AGE.",
								},
								"scopes": map[string]any{
									"type":        "array",
									"description": "OAuth scopes to request.",
									"items": map[string]any{
										"type": "string",
									},
								},
								"redirectURI": map[string]any{
									"type":        "string",
									"description": "Custom redirect URI for the interactive flow (type=oauth only). Must point at a loopback host (127.0.0.1/localhost). Defaults to http://127.0.0.1:<callbackPort>/mcp/oauth/callback.",
								},
								"callbackPort": map[string]any{
									"type":        "integer",
									"description": "Local port for the interactive OAuth callback server (type=oauth only). Defaults to 19876.",
									"default":     19876,
								},
								"authServerMetadataURL": map[string]any{
									"type":        "string",
									"description": "Override the authorization server metadata (RFC 8414 / OIDC discovery) URL instead of auto-discovering it from the MCP server's protected-resource metadata (RFC 9728).",
								},
							},
						},
						"clientCert": map[string]any{
							"type":        "string",
							"description": "Path to a PEM client certificate for mutual TLS. Requires clientKey. Supports ~ and $VAR expansion.",
						},
						"clientKey": map[string]any{
							"type":        "string",
							"description": "Path to the PEM private key matching clientCert. Requires clientCert. Supports ~ and $VAR expansion.",
						},
						"clientKeyPassword": map[string]any{
							"type":        "string",
							"description": "Passphrase for an encrypted clientKey, if any. Encrypted at rest with AGE.",
						},
						"caCert": map[string]any{
							"type":        "string",
							"description": "Path to a PEM CA certificate bundle to trust for this server, in addition to the system trust store. Supports ~ and $VAR expansion.",
						},
						"skipTLSVerify": map[string]any{
							"type":        "boolean",
							"description": "Disable TLS certificate validation entirely. Insecure: only use for local development/testing against a known-trusted server.",
							"default":     false,
						},
					},
				},
			},
		},
	}

	// Add providers
	providerSchema := map[string]any{
		"type":        "object",
		"description": "LLM provider configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "Provider configuration",
			"properties": map[string]any{
				"apiKey": map[string]any{
					"type":        "string",
					"description": "API key for the provider",
				},
				"disabled": map[string]any{
					"type":        "boolean",
					"description": "Whether the provider is disabled",
					"default":     false,
				},
			},
		},
	}

	// Add known providers
	knownProviders := []string{
		string(models.ProviderAnthropic),
		string(models.ProviderOpenAI),
		string(models.ProviderGemini),
		string(models.ProviderGROQ),
		string(models.ProviderOpenRouter),
		string(models.ProviderBedrock),
		string(models.ProviderAzure),
		string(models.ProviderVertexAI),
	}

	providerSchema["additionalProperties"].(map[string]any)["properties"].(map[string]any)["provider"] = map[string]any{
		"type":        "string",
		"description": "Provider type",
		"enum":        knownProviders,
	}

	schema["properties"].(map[string]any)["providers"] = providerSchema

	// Add agents
	agentSchema := map[string]any{
		"type":        "object",
		"description": "Agent configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "Agent configuration",
			"properties": map[string]any{
				"model": map[string]any{
					"type":        "string",
					"description": "Model ID for the agent",
				},
				"maxTokens": map[string]any{
					"type":        "integer",
					"description": "Maximum tokens for the agent",
					"minimum":     1,
				},
				"reasoningEffort": map[string]any{
					"type":        "string",
					"description": "Reasoning effort for models that support it (OpenAI, Anthropic)",
					"enum":        []string{"low", "medium", "high"},
				},
			},
			"required": []string{"model"},
		},
	}

	// Add model enum
	modelEnum := []string{}
	for modelID := range models.SupportedModels {
		modelEnum = append(modelEnum, string(modelID))
	}
	agentSchema["additionalProperties"].(map[string]any)["properties"].(map[string]any)["model"].(map[string]any)["enum"] = modelEnum

	// Add specific agent properties
	agentProperties := map[string]any{}
	knownAgents := []string{
		string(config.AgentCoder),
		string(config.AgentTask),
		string(config.AgentTitle),
	}

	for _, agentName := range knownAgents {
		agentProperties[agentName] = map[string]any{
			"$ref": "#/definitions/agent",
		}
	}

	// Create a combined schema that allows both specific agents and additional ones
	combinedAgentSchema := map[string]any{
		"type":                 "object",
		"description":          "Agent configurations",
		"properties":           agentProperties,
		"additionalProperties": agentSchema["additionalProperties"],
	}

	schema["properties"].(map[string]any)["agents"] = combinedAgentSchema
	schema["definitions"] = map[string]any{
		"agent": agentSchema["additionalProperties"],
	}

	// Add LSP configuration
	schema["properties"].(map[string]any)["lsp"] = map[string]any{
		"type":        "object",
		"description": "Language Server Protocol configurations",
		"additionalProperties": map[string]any{
			"type":        "object",
			"description": "LSP configuration for a language",
			"properties": map[string]any{
				"disabled": map[string]any{
					"type":        "boolean",
					"description": "Whether the LSP is disabled",
					"default":     false,
				},
				"command": map[string]any{
					"type":        "string",
					"description": "Command to execute for the LSP server",
				},
				"args": map[string]any{
					"type":        "array",
					"description": "Command arguments for the LSP server",
					"items": map[string]any{
						"type": "string",
					},
				},
				"options": map[string]any{
					"type":        "object",
					"description": "Additional options for the LSP server",
				},
			},
			"required": []string{"command"},
		},
	}

	// Add ACP configuration (stdio server for editor integrations)
	schema["properties"].(map[string]any)["acp"] = map[string]any{
		"type":        "object",
		"description": "ACP (Agent Client Protocol) server configuration. Controls how Pando behaves when launched as a subprocess by editors like VS Code, Zed, or JetBrains.",
		"properties": map[string]any{
			"enabled": map[string]any{
				"type":        "boolean",
				"description": "Enable ACP server mode",
				"default":     true,
			},
			"max_sessions": map[string]any{
				"type":        "integer",
				"description": "Maximum number of concurrent ACP sessions",
				"default":     10,
				"minimum":     1,
			},
			"idle_timeout": map[string]any{
				"type":        "string",
				"description": "Duration before an idle session is cleaned up (e.g. '30m', '1h')",
				"default":     "30m",
			},
			"log_level": map[string]any{
				"type":        "string",
				"description": "Logging verbosity for the ACP server",
				"default":     "info",
				"enum":        []string{"debug", "info", "warn", "error"},
			},
			"auto_permission": map[string]any{
				"type":        "boolean",
				"description": "Automatically approve tool permission requests (for CI/batch environments)",
				"default":     false,
			},
		},
	}

	return schema
}
