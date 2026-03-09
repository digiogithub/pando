package models

const (
	ProviderOpenAI ModelProvider = "openai"

	// Model IDs match the dynamic format: "openai.{api-id}"
	GPT41        ModelID = "openai.gpt-4.1"
	GPT41Mini    ModelID = "openai.gpt-4.1-mini"
	GPT41Nano    ModelID = "openai.gpt-4.1-nano"
	GPT45Preview ModelID = "openai.gpt-4.5-preview"
	GPT4o        ModelID = "openai.gpt-4o"
	GPT4oMini    ModelID = "openai.gpt-4o-mini"
	O1           ModelID = "openai.o1"
	O1Pro        ModelID = "openai.o1-pro"
	O1Mini       ModelID = "openai.o1-mini"
	O3           ModelID = "openai.o3"
	O3Mini       ModelID = "openai.o3-mini"
	O4Mini       ModelID = "openai.o4-mini"
)
