package api

import "context"

// ImageGenerationAPI names an image generation wire protocol. Like
// TranscriptionAPI it is its own type, so no other modality's value
// validates.
type ImageGenerationAPI string

// ImageGenerationOpenAI is OpenAI's Images API: a JSON POST to
// {base}/images/generations for text-to-image and a multipart POST to
// {base}/images/edits carrying the reference images, both answering data[]
// with b64_json or url.
const ImageGenerationOpenAI ImageGenerationAPI = "openai-images"

// Known reports whether the value names a protocol this build implements.
func (a ImageGenerationAPI) Known() bool { return a == ImageGenerationOpenAI }

// ImageGenerationInput is one reference image, sent as the vendor received
// it from the user.
type ImageGenerationInput struct {
	Data     []byte
	MIMEType string
	FileName string
}

// ImageGenerationRequest is one generation call, already validated.
type ImageGenerationRequest struct {
	Prompt string
	// Images empty means text-to-image.
	Images []ImageGenerationInput
	// Size is sent as is; empty sends none.
	Size string
	// Count is sent as n when above 1.
	Count int
}

// GeneratedImage is one image of the reply, decoded or downloaded.
type GeneratedImage struct {
	Data          []byte
	MIMEType      string
	RevisedPrompt string
}

// ImageGenerationClient is what a protocol client implements.
type ImageGenerationClient interface {
	GenerateImages(ctx context.Context, req ImageGenerationRequest) ([]GeneratedImage, error)
}
