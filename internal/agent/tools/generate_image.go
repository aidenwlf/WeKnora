package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	generateImageMaxCount = 4
	// Generation commonly takes 30-120 s per call, plus reading references
	// that may still be parsing.
	generateImageTimeout = 5 * time.Minute
	// GenerateImageDisplayType marks generate_image results for the chat card
	// and for deriving message artifacts.
	GenerateImageDisplayType = "image_generation"
)

var generateImageTool = BaseTool{
	name: ToolGenerateImage,
	description: `Generate images from a text description, or edit and combine reference images.

## When to Use

- The user asks for a picture, illustration, poster, icon, mock-up or photo
- The user asks to change, restyle, extend or combine images they sent or that
  were generated earlier in this conversation (pass them in "images")

## Writing the Prompt

Write one complete visual description in the user's language: subject, setting,
composition, style, colours, lighting and any text that must appear. For an edit,
say exactly what to change and what to keep. Do not just repeat the user's words
when they are brief; fill in the details a designer would need.

## Reference Images

"images" accepts, in order:
- attachment://att_xxxx — an image the user uploaded (its first image), or
  attachment://att_xxxx/images/N for image N inside an uploaded document
- an image reference already shown in this conversation (res://NNNN), such as a
  picture the user sent or one this tool generated

Leave "images" empty for a new image from text alone.

## Result

The images are saved and shown to the user automatically. The result lists each
image's reference, which you can embed as ![description](reference) or pass back
in "images" to edit it further.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "prompt": {
      "type": "string",
      "description": "Complete visual description of the new image or of the edit, in the user's language"
    },
    "images": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Optional images to edit or combine: attachment://att_xxxx[/images/N] or a res://NNNN image"
    },
    "size": {
      "type": "string",
      "description": "Optional output size as WIDTHxHEIGHT, e.g. 1024x1024; omit for the model's default"
    },
    "count": {
      "type": "integer",
      "minimum": 1,
      "maximum": 4,
      "description": "How many images to generate (default 1, max 4)"
    }
  },
  "required": ["prompt"]
}`),
}

// GeneratedImageStore persists generated images and authorizes resource://
// references to earlier images. Implementations scope both to one session.
type GeneratedImageStore interface {
	// Save stores one generated image and returns its resource:// reference.
	Save(ctx context.Context, data []byte, fileName string) (string, error)
	// Read returns the original bytes of an image this session may reuse.
	Read(ctx context.Context, ref string) (*imagegen.InputImage, error)
}

// AttachmentImageSource reads the original bytes of an uploaded image, named
// by attachment and number or by the stored reference its text shows.
type AttachmentImageSource interface {
	ReadOriginalImage(ctx context.Context, ref string, number int) (*imagegen.InputImage, error)
	ReadOriginalImageByURL(ctx context.Context, url string) (*imagegen.InputImage, error)
}

// GenerateImageInput defines the input parameters for generate_image.
type GenerateImageInput struct {
	Prompt string   `json:"prompt"`
	Images []string `json:"images,omitempty"`
	Size   string   `json:"size,omitempty"`
	Count  int      `json:"count,omitempty"`
}

// GenerateImageTool generates and edits images with the agent's image model.
type GenerateImageTool struct {
	BaseTool
	generator   imagegen.ImageGenerator
	store       GeneratedImageStore
	attachments AttachmentImageSource
}

// NewGenerateImageTool binds the tool to one model and one session's storage.
func NewGenerateImageTool(generator imagegen.ImageGenerator, store GeneratedImageStore) *GenerateImageTool {
	return &GenerateImageTool{BaseTool: generateImageTool, generator: generator, store: store}
}

// WithAttachments lets references name the session's uploaded images. The
// attachment reader is built after the tool, so it is attached later.
func (t *GenerateImageTool) WithAttachments(source AttachmentImageSource) *GenerateImageTool {
	t.attachments = source
	return t
}

// ExecutionTimeout lifts the engine's default deadline for slow image models.
func (t *GenerateImageTool) ExecutionTimeout() time.Duration { return generateImageTimeout }

// Execute generates the images, stores them and reports their references.
func (t *GenerateImageTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	var input GenerateImageInput
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{Success: false, Error: fmt.Sprintf("Failed to parse args: %v", err)}, nil
	}
	req, refusal := t.request(input)
	if refusal != "" {
		return &types.ToolResult{Success: false, Error: refusal}, nil
	}
	for _, ref := range input.Images {
		image, err := t.loadReference(ctx, strings.TrimSpace(ref))
		if err != nil {
			return &types.ToolResult{Success: false, Error: fmt.Sprintf("Reference image %s: %v", ref, err)}, nil
		}
		req.Images = append(req.Images, *image)
	}
	if meta, ok := ToolExecFromContext(ctx); ok {
		logger.Infof(ctx, "generate_image: session=%s call=%s model=%s references=%d count=%d",
			meta.SessionID, meta.ToolCallID, t.generator.GetModelID(), len(req.Images), req.Count)
	}
	result, err := t.generator.Generate(ctx, req)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "Image generation failed: " + err.Error()}, err
	}
	if result == nil || len(result.Images) == 0 {
		return &types.ToolResult{Success: false, Error: "The image model returned no image; " +
			"try a different prompt"}, nil
	}
	saved, err := t.save(ctx, result.Images)
	if err != nil {
		return &types.ToolResult{Success: false, Error: "Could not save the generated image: " + err.Error()}, err
	}
	return generateImageResult(req, t.generator.GetModelName(), saved), nil
}

// request validates the arguments the model chose; a non-empty refusal goes
// back to the model so it can correct them.
func (t *GenerateImageTool) request(input GenerateImageInput) (imagegen.Request, string) {
	req := imagegen.Request{Prompt: strings.TrimSpace(input.Prompt), Count: input.Count}
	if req.Prompt == "" {
		return req, "prompt is required"
	}
	if req.Count == 0 {
		req.Count = 1
	}
	if req.Count < 1 || req.Count > generateImageMaxCount {
		return req, fmt.Sprintf("count must be between 1 and %d", generateImageMaxCount)
	}
	size, ok := normalizeImageSize(input.Size)
	if !ok {
		return req, fmt.Sprintf("size %q must be WIDTHxHEIGHT, for example 1024x1024", input.Size)
	}
	req.Size = size
	limit := t.generator.MaxInputImages()
	switch {
	case len(input.Images) > 0 && limit == 0:
		return req, "the configured image model only generates images from text; " +
			"call again without images and describe the picture in the prompt"
	case len(input.Images) > limit:
		return req, fmt.Sprintf("the configured image model accepts at most %d reference image(s); got %d",
			limit, len(input.Images))
	}
	return req, ""
}
