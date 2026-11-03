// Package providers registers the WunoOS OpenAI-compatible gateway.
//
// Facts (https://api.wunoai.com/v1, new-api relay):
//   - the gateway speaks OpenAI Chat Completions at /v1 and takes a bearer
//     token; it currently serves the DeepSeek V4.1 Flash family;
//   - upstream is DeepSeek, so the request shape follows the first-party
//     surface: `max_tokens` for the output cap, `thinking.type` for the
//     switch, `reasoning_effort` low/high/max, and `reasoning_content`
//     replay is handled by the protocol layer;
//   - only chat is offered by the gateway's pricing catalog; embedding,
//     rerank and speech-to-text rows would 404, so they are not declared.
package providers

import (
	_ "embed"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/types"
)

//go:embed assets/wunoos.svg
var wunoosIcon []byte

// WunoosID is the provider identifier stored on model rows.
const WunoosID = "wunoos"

// WunoosBaseURL is the gateway's OpenAI-compatible endpoint.
const WunoosBaseURL = "https://api.wunoai.com/v1"

func newWunoosProvider() *Definition {
	return &Definition{
		ID:           WunoosID,
		Name:         "WunoOS",
		Names:        map[string]string{"zh-CN": "WunoOS"},
		Description:  "deepseek-v4.1-flash",
		Website:      "https://wunoai.com",
		Icon:         wunoosIcon,
		API:          api.APIOpenAICompletions,
		Order:        35,
		RequiresAuth: true,
		Auth:         AuthBearer,
		URLPatterns:  []string{"api.wunoai.com"},
		DefaultBaseURLs: map[types.ModelType]string{
			types.ModelTypeKnowledgeQA: WunoosBaseURL,
			types.ModelTypeVLLM:        WunoosBaseURL,
		},
		ModelTypes: []types.ModelType{
			types.ModelTypeKnowledgeQA,
			types.ModelTypeVLLM,
		},
		Compat: VendorCompat{
			OpenAICompletions: api.OpenAICompletionsCompat{
				MaxTokensField:          api.Ptr("max_tokens"),
				ThinkingFormat:          api.Ptr(api.ThinkingFormatThinkingType),
				SupportsReasoningEffort: api.Ptr(true),
				PromptCacheAccounting:   api.Ptr(true),
				SupportsStore:           api.Ptr(false),
				// DeepSeek rejects required/named tool choices while thinking
				// is on, which is the default on the served model.
				ToolChoiceModes: []string{"none", "auto"},
			},
		},
		ThinkingLevels: api.ThinkingLevelMap{
			api.ReasoningMinimal: api.StringPtr("low"),
			api.ReasoningLow:     api.StringPtr("low"),
			api.ReasoningMedium:  api.StringPtr("high"),
			api.ReasoningHigh:    api.StringPtr("high"),
			api.ReasoningXHigh:   api.StringPtr("max"),
			api.ReasoningMax:     api.StringPtr("max"),
		},
	}
}
