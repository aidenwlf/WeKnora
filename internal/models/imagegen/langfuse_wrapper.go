package imagegen

import (
	"context"

	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
)

// langfuseGenerator reports each Generate call as a Langfuse generation
// observation. No image bytes are uploaded; usage is the image count, which
// is how most vendors bill image generation.
type langfuseGenerator struct {
	inner ImageGenerator
}

func (l *langfuseGenerator) GetModelName() string { return l.inner.GetModelName() }
func (l *langfuseGenerator) GetModelID() string   { return l.inner.GetModelID() }
func (l *langfuseGenerator) MaxInputImages() int  { return l.inner.MaxInputImages() }

func (l *langfuseGenerator) Generate(ctx context.Context, req Request) (*Result, error) {
	mgr := langfuse.GetManager()
	if !mgr.Enabled() {
		return l.inner.Generate(ctx, req)
	}
	genCtx, gen := mgr.StartGeneration(ctx, langfuse.GenerationOptions{
		Name:  "image.generate",
		Model: l.inner.GetModelName(),
		Input: map[string]interface{}{
			"prompt":            req.Prompt,
			"size":              req.Size,
			"count":             req.Count,
			"input_image_count": len(req.Images),
		},
		Metadata: map[string]interface{}{"model_id": l.inner.GetModelID()},
	})
	result, err := l.inner.Generate(genCtx, req)

	output := map[string]interface{}{}
	var usage *langfuse.TokenUsage
	if result != nil {
		n := len(result.Images)
		output["image_count"] = n
		usage = &langfuse.TokenUsage{Output: n, Total: n, Unit: "IMAGES"}
	}
	gen.Finish(output, usage, err)
	return result, err
}

func wrapLangfuse(g ImageGenerator, err error) (ImageGenerator, error) {
	if err != nil || g == nil {
		return g, err
	}
	if !langfuse.GetManager().Enabled() {
		return g, nil
	}
	return &langfuseGenerator{inner: g}, nil
}
