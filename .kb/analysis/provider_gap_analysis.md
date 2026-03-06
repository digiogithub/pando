# Pando Provider Gap Analysis: Plan vs Reality

## Summary
The ACP multi-agent plan (Phase 1) proposes a provider system update. Analysis reveals Pando already implements ~70% of what's proposed.

## Already Implemented
| Feature | Status | Location |
|---------|--------|----------|
| Unified Provider interface | Done | provider.go:53-59 |
| Factory pattern (NewProvider) | Done | provider.go:86-168 |
| Anthropic provider | Done | anthropic.go (with cache, thinking) |
| OpenAI provider | Done | openai.go (with reasoning effort) |
| Gemini provider | Done | gemini.go |
| Bedrock provider | Done | bedrock.go |
| Groq provider | Done | OpenAI-compat with groq base URL |
| OpenRouter provider | Done | OpenAI-compat with openrouter base URL |
| Azure provider | Done | azure.go |
| VertexAI provider | Done | vertexai.go |
| Copilot provider | Done | copilot.go |
| XAI provider | Done | OpenAI-compat with x.ai base URL |
| Local provider | Done | OpenAI-compat with LOCAL_ENDPOINT |
| Model cost tracking | Done | CostPer1MIn/Out/Cached in models |
| Generic base provider | Done | baseProvider[C ProviderClient] |

## Gaps to Fill
| Feature | Priority | Notes |
|---------|----------|-------|
| Vercel AI Gateway | Medium | Add as OpenAI-compat with api.vercel.ai |
| Provider Capabilities | Low | Add Capabilities() to interface |
| ValidateConfig | Low | Add config validation per provider |
| Dynamic Registry | Medium | Allow runtime provider registration |
| Multi-layer config merge | Medium | Crush pattern: defaults -> provider -> model |
| Mid-session model switching | High | Preserve context when changing models |
| Remote model catalog | Low | Optional remote catalog with embedded fallback |

## Recommended Next Steps for Phase 1
1. Add Vercel provider (simple OpenAI-compat)
2. Enrich Provider interface with Capabilities() and ValidateConfig()
3. Implement provider registry for dynamic registration
4. Add multi-layer config merging
5. Focus effort on Phase 2 (ACP) and Phase 3 (TUI) instead