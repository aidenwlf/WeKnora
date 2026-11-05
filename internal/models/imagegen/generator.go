package imagegen

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/api"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// generator adapts a protocol client to ImageGenerator and refuses a request
// the model has documented it will not take, before it is sent and billed.
type generator struct {
	inner     api.ImageGenerationClient
	settings  api.ImagesSettings
	vendor    string
	endpoint  string
	modelName string
	modelID   string
}

func (g *generator) GetModelName() string { return g.modelName }
func (g *generator) GetModelID() string   { return g.modelID }
func (g *generator) MaxInputImages() int  { return g.settings.MaxInputImages }

// sizes is what CheckSize reads through the langfuse wrapper.
func (g *generator) sizes() []string { return g.settings.Sizes }

func (g *generator) Generate(ctx context.Context, req Request) (*Result, error) {
	wire, err := g.wireRequest(req)
	if err != nil {
		return nil, err
	}
	logger.Infof(ctx, "[ImageGen] generating model=%s endpoint=%s count=%d size=%q input_images=%d",
		g.modelName, g.endpoint, wire.Count, wire.Size, len(wire.Images))
	images, err := g.inner.GenerateImages(ctx, wire)
	if err != nil {
		return nil, fmt.Errorf("image generation request failed: %w", err)
	}
	result := &Result{Images: make([]Image, 0, len(images))}
	for _, img := range images {
		result.Images = append(result.Images, Image{
			Data: img.Data, MIMEType: img.MIMEType, RevisedPrompt: img.RevisedPrompt,
		})
	}
	logger.Infof(ctx, "[ImageGen] generation completed, images=%d", len(result.Images))
	return result, nil
}

// wireRequest validates req against the resolved settings.
func (g *generator) wireRequest(req Request) (api.ImageGenerationRequest, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return api.ImageGenerationRequest{}, fmt.Errorf("image generation: the prompt is empty")
	}
	count := req.Count
	if count == 0 {
		count = 1
	}
	if count < 1 || count > g.settings.MaxCount {
		return api.ImageGenerationRequest{}, fmt.Errorf("image generation: %s generates 1 to %d images per call, not %d",
			g.modelName, g.settings.MaxCount, count)
	}
	if err := g.checkImages(req.Images); err != nil {
		return api.ImageGenerationRequest{}, err
	}
	size := strings.TrimSpace(req.Size)
	if size == "" {
		size = g.settings.DefaultSize
	}
	if sizes := g.settings.Sizes; size != "" && len(sizes) > 0 && !slices.Contains(sizes, size) {
		return api.ImageGenerationRequest{}, fmt.Errorf("image generation: %s accepts sizes %s, not %q",
			g.modelName, strings.Join(sizes, ", "), size)
	}
	wire := api.ImageGenerationRequest{Prompt: prompt, Size: size, Count: count}
	for _, img := range req.Images {
		wire.Images = append(wire.Images, api.ImageGenerationInput{
			Data: img.Data, MIMEType: img.MIMEType, FileName: img.FileName,
		})
	}
	return wire, nil
}

func (g *generator) checkImages(images []InputImage) error {
	limit := g.settings.MaxInputImages
	if len(images) > limit {
		if limit == 0 {
			return fmt.Errorf("image generation: %s (%s) only generates from text and takes no reference images",
				g.modelName, g.vendor)
		}
		return fmt.Errorf("image generation: %s takes at most %d reference images, not %d",
			g.modelName, limit, len(images))
	}
	for i, img := range images {
		if len(img.Data) == 0 {
			return fmt.Errorf("image generation: reference image %d is empty", i+1)
		}
		if !strings.HasPrefix(img.MIMEType, "image/") {
			return fmt.Errorf("image generation: reference image %d is %q, not an image", i+1, img.MIMEType)
		}
	}
	return nil
}

func newHTTPClient(timeout time.Duration) *http.Client {
	cfg := secutils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = timeout
	return secutils.NewSSRFSafeHTTPClient(cfg)
}
