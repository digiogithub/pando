package models

const (
	ProviderAnthropic ModelProvider = "anthropic"

	// Model IDs match the dynamic format: "anthropic.{api-id}"
	Claude35Sonnet ModelID = "anthropic.claude-3-5-sonnet-20241022"
	Claude3Haiku   ModelID = "anthropic.claude-3-haiku-20240307"
	Claude37Sonnet ModelID = "anthropic.claude-3-7-sonnet-20250219"
	Claude35Haiku  ModelID = "anthropic.claude-3-5-haiku-20241022"
	Claude3Opus    ModelID = "anthropic.claude-3-opus-20240229"
	Claude4Opus    ModelID = "anthropic.claude-opus-4-20250514"
	Claude4Sonnet  ModelID = "anthropic.claude-sonnet-4-20250514"
)
