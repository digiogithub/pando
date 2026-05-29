package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/digiogithub/pando/internal/config"
	"github.com/digiogithub/pando/internal/llm/models"
)

func main() {
	cfg, _ := config.Load(".", false)
	if cfg == nil {
		cfg = &config.Config{}
	}
	config.SetForTests(cfg)
	models.LoadModelCache()

	provAcc, _ := config.ResolveProviderAccountForType(models.ProviderOllama)
	refreshParams := models.AccountModelRefreshParams{
		AccountID: provAcc.ID, ProviderType: provAcc.Type, AllAccountsOfType: 1,
	}
	models.RefreshProviderModelsForAccount(context.Background(), refreshParams)

	tests := []struct {
		label     string
		maxTokens int64
	}{
		{"max_tokens=5", 5},
		{"max_tokens=50", 50},
		{"max_tokens=200", 200},
	}

	for _, tt := range tests {
		fmt.Printf("\n=== %s ===\n", tt.label)
		client := oai.NewClient(
			option.WithBaseURL("http://localhost:11434/v1"),
			option.WithAPIKey(""),
		)

		streamParams := oai.ChatCompletionNewParams{
			Model: oai.ChatModel("gemma4-e2b:latest"),
			Messages: []oai.ChatCompletionMessageParamUnion{
				oai.SystemMessage("You are helpful."),
				oai.UserMessage("Say hello"),
			},
			MaxTokens:     oai.Int(tt.maxTokens),
			StreamOptions: oai.ChatCompletionStreamOptionsParam{IncludeUsage: oai.Bool(true)},
		}

		start := time.Now()
		stream := client.Chat.Completions.NewStreaming(context.Background(), streamParams)
		accum := oai.ChatCompletionAccumulator{}
		for stream.Next() {
			chunk := stream.Current()
			accum.AddChunk(chunk)
		}
		err := stream.Err()
		elapsed := time.Since(start)
		
		fmt.Printf("Time: %v, err=%v\n", elapsed, err)
		if len(accum.ChatCompletion.Choices) > 0 {
			choice := accum.ChatCompletion.Choices[0]
			fmt.Printf("FinishReason: %q\n", choice.FinishReason)
			fmt.Printf("Content: %q\n", choice.Message.Content)
			// Print raw JSON to see everything
			raw, _ := json.Marshal(choice)
			fmt.Printf("Raw: %s\n", string(raw)[:min(200, len(string(raw)))])
		}
		stream.Close()
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
