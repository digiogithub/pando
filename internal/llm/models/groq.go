package models

const (
	ProviderGROQ ModelProvider = "groq"

	// Model IDs match the dynamic format: "groq.{api-id}"
	QWENQwq                   ModelID = "groq.qwen-qwq-32b"
	Llama4Scout               ModelID = "groq.meta-llama/llama-4-scout-17b-16e-instruct"
	Llama4Maverick            ModelID = "groq.meta-llama/llama-4-maverick-17b-128e-instruct"
	Llama3_3_70BVersatile     ModelID = "groq.llama-3.3-70b-versatile"
	DeepseekR1DistillLlama70b ModelID = "groq.deepseek-r1-distill-llama-70b"
)
