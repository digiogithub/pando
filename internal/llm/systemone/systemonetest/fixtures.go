package systemonetest

// Recorded fixtures of Ollama 0.35.0 (trimmed). Numbers keep full float
// precision on purpose: clients must not assume any rounding.

// OllamaVersionJSON is the /api/version body.
const OllamaVersionJSON = `{"version":"0.35.0"}`

// OllamaTagsJSON is the /api/tags body used by NewOllama035.
const OllamaTagsJSON = `{"models":[
{"name":"tev1:0.8b","model":"tev1:0.8b","size":811856202,"details":{"family":"qwen35","parameter_size":"752.39M","quantization_level":"Q8_0","context_length":262144},"capabilities":["decision","tools","thinking","completion"]},
{"name":"qwen2.5-coder:0.5b","model":"qwen2.5-coder:0.5b","size":397821319,"details":{"family":"qwen2","parameter_size":"494.03M","quantization_level":"Q4_K_M","context_length":32768},"capabilities":["completion","tools","insert"]},
{"name":"nomic-embed-text:latest","model":"nomic-embed-text:latest","size":274302450,"details":{"family":"nomic-bert","parameter_size":"137M","quantization_level":"F16","context_length":2048},"capabilities":["embedding"]},
{"name":"glm-5.1:cloud","model":"glm-5.1:cloud","size":0,"details":{"family":"glm","context_length":131072},"capabilities":["completion","tools"]}
]}`

// OllamaShowTev1JSON is the /api/show body for tev1:0.8b.
const OllamaShowTev1JSON = `{"parameters":"num_ctx                        2050","model_info":{"qwen35.context_length":262144},"capabilities":["decision","tools","thinking","completion"]}`

// DuplicateChargeResponseJSON is the recorded /v1/systemone answer for a
// billing-intent question.
const DuplicateChargeResponseJSON = `{"model":"tev1:0.8b","answers":{"intent":{"type":"choice","choice":"duplicate_charge","probabilities":{"duplicate_charge":0.9760968387170561,"refund_request":0.0155031612829439,"other":0.0084},"confidence":0.9145015492062412}},"usage":{"input_tokens":111,"output_tokens":1}}`

// Recorded probability values of DuplicateChargeResponseJSON.
const (
	DuplicateChargeP          = 0.9760968387170561
	DuplicateChargeConfidence = 0.9145015492062412
)

// OpenRouterModelsJSON is an OpenAI-style mixed catalogue.
const OpenRouterModelsJSON = `{"data":[{"id":"openai/gpt-5"},{"id":"typesafe/jev-1.13"},{"id":"~typesafe/jev-latest"}]}`

// TypeSafeModelsJSON is the {"models":[...]} shape.
const TypeSafeModelsJSON = `{"models":["jev-latest","jev-1.13"]}`
