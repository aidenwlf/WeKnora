package imagegen

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/models"
	"github.com/stretchr/testify/require"
)

func generate(t *testing.T, cfg *Config, req Request) (*Result, error) {
	t.Helper()
	g, err := New(cfg)
	require.NoError(t, err)
	return g.Generate(context.Background(), req)
}

// Zhipu's CogView and Ark's Seedream generate from text only in this build,
// so a reference image is refused before anything is sent and billed.
func TestTextOnlyVendorsRefuseReferenceImages(t *testing.T) {
	for _, tc := range []struct{ provider, model, path string }{
		{"zhipu", "cogview-4-250304", "/api/paas/v4"},
		{"volcengine", "doubao-seedream-4-0-250828", "/api/v3"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			address, _, calls := upstream(t)
			g, err := New(&Config{Provider: tc.provider, ModelName: tc.model, BaseURL: address + tc.path, APIKey: "k"})
			require.NoError(t, err)
			require.Zero(t, g.MaxInputImages())
			_, err = g.Generate(context.Background(), Request{Prompt: "p", Images: []InputImage{reference()}})
			require.ErrorContains(t, err, "takes no reference images")
			require.Zero(t, calls.Load())
		})
	}
}

// gpt-image models take several reference images, sent as image[] parts
// with their own content types.
func TestOpenAIGPTImageEditsWithSeveralImages(t *testing.T) {
	address, got, calls := upstream(t)
	cfg := &Config{Provider: "openai", ModelName: "gpt-image-2", BaseURL: address + "/v1", APIKey: "k"}
	_, err := generate(t, cfg, Request{Prompt: "combine", Images: []InputImage{reference(), reference()}, Count: 2})
	require.NoError(t, err)
	require.Equal(t, "/v1/images/edits", got.path)
	require.Equal(t, "Bearer k", got.auth)
	require.Equal(t, map[string]string{"model": "gpt-image-2", "prompt": "combine", "n": "2"}, got.fields)
	require.Equal(t, []string{"image[]:image/png", "image[]:image/png"}, got.files)

	g, err := New(cfg)
	require.NoError(t, err)
	tooMany := make([]InputImage, g.MaxInputImages()+1)
	for i := range tooMany {
		tooMany[i] = reference()
	}
	_, err = g.Generate(context.Background(), Request{Prompt: "p", Images: tooMany})
	require.ErrorContains(t, err, "at most")
	require.EqualValues(t, 1, calls.Load())
}

// A self-hosted gateway's model is not in the catalog; it may take reference
// images and the gateway's own error decides.
func TestUncataloguedGenericModelAllowsReferenceImages(t *testing.T) {
	address, got, _ := upstream(t)
	_, err := generate(t, &Config{Provider: "generic", ModelName: "flux-kontext-dev", BaseURL: address + "/v1"},
		Request{Prompt: "p", Images: []InputImage{reference()}})
	require.NoError(t, err)
	require.Equal(t, "/v1/images/edits", got.path)
	require.Equal(t, []string{"image:image/png"}, got.files)
}

// Azure carries the deployment in the body on the v1 data plane and in the
// path on the legacy one, with the api-key header either way.
func TestAzureRoutesBothDataPlanes(t *testing.T) {
	for _, tc := range []struct {
		name, version, genPath, editPath, query string
	}{
		{"v1", "", "/openai/v1/images/generations", "/openai/v1/images/edits", "api-version=preview"},
		{"deployments", "2025-04-01-preview", "/openai/deployments/my-image/images/generations",
			"/openai/deployments/my-image/images/edits", "api-version=2025-04-01-preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			address, got, _ := upstream(t)
			cfg := &Config{Provider: "azure_openai", ModelName: "my-image", BaseURL: address, APIKey: "az"}
			if tc.version != "" {
				cfg.ExtraConfig = map[string]string{models.ExtraAPIVersion: tc.version}
			}
			_, err := generate(t, cfg, Request{Prompt: "p"})
			require.NoError(t, err)
			require.Equal(t, tc.genPath, got.path)
			require.Equal(t, tc.query, got.query)
			require.Equal(t, "az", got.apiKey)
			require.Empty(t, got.auth)
			require.Equal(t, "my-image", got.body["model"])

			_, err = generate(t, cfg, Request{Prompt: "p", Images: []InputImage{reference()}})
			require.NoError(t, err)
			require.Equal(t, tc.editPath, got.path)
			require.Equal(t, tc.query, got.query)
		})
	}
}

func TestRemoteModelNameIsSentOnTheWire(t *testing.T) {
	address, got, _ := upstream(t)
	_, err := generate(t, &Config{
		Provider: "openai", ModelName: "my-label", BaseURL: address + "/v1", APIKey: "k",
		ExtraConfig: map[string]string{models.ExtraRemoteModelName: "gpt-image-2"},
	}, Request{Prompt: "p"})
	require.NoError(t, err)
	require.Equal(t, "gpt-image-2", got.body["model"])
}

func TestRequestsTheModelCannotServeAreRefusedBeforeSending(t *testing.T) {
	for name, tc := range map[string]struct {
		provider, model, path string
		req                   Request
		want                  string
	}{
		"empty prompt":   {"openai", "gpt-image-2", "/v1", Request{Prompt: "  "}, "prompt is empty"},
		"count above 10": {"openai", "gpt-image-2", "/v1", Request{Prompt: "p", Count: 11}, "1 to 10 images"},
		"negative count": {"openai", "gpt-image-2", "/v1", Request{Prompt: "p", Count: -1}, "1 to 10 images"},
		// CogView answers one image and documents no n.
		"zhipu n=1 only": {"zhipu", "cogview-4", "/api/paas/v4", Request{Prompt: "p", Count: 2}, "1 to 1 images"},
		"size outside list": {"openai", "gpt-image-1.5", "/v1", Request{Prompt: "p", Size: "640x480"},
			"accepts sizes"},
		"not an image": {"openai", "gpt-image-2", "/v1", Request{Prompt: "p", Images: []InputImage{
			{Data: []byte("%PDF"), MIMEType: "application/pdf"},
		}}, "not an image"},
	} {
		t.Run(name, func(t *testing.T) {
			address, _, calls := upstream(t)
			_, err := generate(t, &Config{Provider: tc.provider, ModelName: tc.model, BaseURL: address + tc.path},
				tc.req)
			require.ErrorContains(t, err, tc.want)
			require.Zero(t, calls.Load())
		})
	}
}

// A vendor that does not declare image generation is refused when the row
// names it.
func TestNamedVendorWithoutImageGenerationIsRefused(t *testing.T) {
	_, err := New(&Config{Provider: "deepseek", ModelName: "deepseek-chat", APIKey: "k"})
	require.ErrorContains(t, err, "does not offer image generation")
}

// The smallest documented size is what a connection check asks for.
func TestCheckSizeIsTheSmallestDocumentedSize(t *testing.T) {
	address, _, _ := upstream(t)
	for model, want := range map[string]string{"gpt-image-1.5": "1024x1024", "gpt-image-2": ""} {
		g, err := New(&Config{Provider: "openai", ModelName: model, BaseURL: address + "/v1", APIKey: "k"})
		require.NoError(t, err)
		require.Equal(t, want, CheckSize(g), model)
	}
}

// The model card's input list is what Generate enforces, whatever the
// catalog entry says: reference images exactly when the limit is above 0.
func TestReportedInputMatchesTheEnforcedLimit(t *testing.T) {
	for _, tc := range []struct {
		provider, model string
		want            []string
	}{
		{"openai", "gpt-image-2", []string{"text", "image"}},
		{"generic", "uncatalogued-gateway-model", []string{"text", "image"}},
		{"zhipu", "cogview-4", []string{"text"}},
		{"volcengine", "doubao-seedream-5-0-pro-260628", []string{"text"}},
	} {
		resolved, err := Resolve(&Config{Provider: tc.provider, ModelName: tc.model, BaseURL: "https://gw.example/v1"})
		require.NoError(t, err, tc.model)
		require.Equal(t, tc.want, resolved.Capabilities().Input, tc.model)
	}
}
