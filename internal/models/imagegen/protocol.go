package imagegen

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/openaiimages"
	"github.com/Tencent/WeKnora/internal/models/providers"
	modelruntime "github.com/Tencent/WeKnora/internal/models/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// retryPolicy retries nothing: a generation that timed out may still have
// been billed, and a client-level retry of a 300-second call would multiply
// both the wait and the cost.
var retryPolicy = func() api.RetryPolicy { return api.RetryPolicy{} }

// newGenerator mirrors asr.newASR: the vendor's facts decide the protocol,
// the URL and the credential, and this function knows no vendor names.
func newGenerator(config *Config) (ImageGenerator, error) {
	if config == nil {
		return nil, fmt.Errorf("image generation config is nil")
	}
	if strings.TrimSpace(config.ModelName) == "" {
		return nil, fmt.Errorf("model name is required")
	}
	resolved, err := Resolve(config)
	if err != nil {
		return nil, err
	}
	if err := validateURL(resolved.BaseURL); err != nil {
		return nil, err
	}
	timeout := time.Duration(resolved.Images.RequestTimeout) * time.Second
	endpoint, err := resolved.Endpoint(types.ModelTypeImageGeneration, modelruntime.Connection{
		ModelID:     config.ModelID,
		Credentials: api.Credentials{APIKey: config.APIKey},
		Headers:     config.CustomHeaders,
		Extra:       config.ExtraConfig,
		Client:      newHTTPClient(timeout),
	})
	if err != nil {
		return nil, err
	}
	if endpoint.URL != "" {
		if err := validateURL(endpoint.URL); err != nil {
			return nil, err
		}
	}

	var client api.ImageGenerationClient
	switch resolved.ImageGenerationAPI {
	case api.ImageGenerationOpenAI:
		client = openaiimages.New(openaiimages.Config{
			Endpoint: endpoint, Settings: resolved.Images, Retry: retryPolicy(),
		})
	default:
		return nil, fmt.Errorf("unsupported image generation api %q for provider %s",
			resolved.ImageGenerationAPI, resolved.Vendor.ID)
	}
	return &generator{
		inner:     client,
		settings:  resolved.Images,
		vendor:    resolved.Vendor.Name,
		endpoint:  resolved.BaseURL,
		modelName: config.ModelName,
		modelID:   config.ModelID,
	}, nil
}

// Resolve is the catalog view New builds from. It refuses a named vendor
// that does not declare image generation: it has not been checked for it,
// and its Endpoint hook may still compute a path. A row matched only by its
// URL is served as an OpenAI-compatible endpoint at that URL, like an ASR
// row.
func Resolve(config *Config) (*modelruntime.Resolved, error) {
	ref := modelruntime.Ref{
		Provider:  config.Provider,
		Model:     config.ModelName,
		BaseURL:   config.BaseURL,
		ModelType: types.ModelTypeImageGeneration,
		Extra:     config.ExtraConfig,
		Override:  config.Spec,
	}
	resolved, err := modelruntime.Resolve(ref)
	if err != nil {
		return nil, err
	}
	if resolved.Vendor.SupportsType(types.ModelTypeImageGeneration) {
		return resolved, nil
	}
	if strings.TrimSpace(config.Provider) != "" {
		return nil, fmt.Errorf("%s does not offer image generation in this build", resolved.Vendor.Name)
	}
	ref.Provider = providers.GenericID
	return modelruntime.Resolve(ref)
}

func validateURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	if err := secutils.ValidateURLForSSRF(rawURL); err != nil {
		return fmt.Errorf("base URL SSRF check failed: %w", err)
	}
	return nil
}
