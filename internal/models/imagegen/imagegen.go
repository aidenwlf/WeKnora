// Package imagegen is the consumer-facing contract for image generation
// models: text-to-image, and image-to-image when the model takes reference
// images. Wire formats live in internal/models/api; this package resolves a
// model row to a protocol client.
package imagegen

import "context"

// InputImage is one reference image for image-to-image generation.
type InputImage struct {
	// Data is the original image bytes, not a downscaled preview: editing
	// works on the pixels it is given.
	Data []byte
	// MIMEType is the image media type, for example image/png.
	MIMEType string
	// FileName carries an extension servers identify the format by.
	FileName string
}

// Request is one generation call.
type Request struct {
	Prompt string
	// Images are the reference images; empty means text-to-image.
	Images []InputImage
	// Size is WIDTHxHEIGHT or a vendor size keyword; empty uses the model's
	// default.
	Size string
	// Count is how many images to generate; 0 means 1.
	Count int
}

// Image is one generated image, already decoded or downloaded.
type Image struct {
	Data     []byte
	MIMEType string
	// RevisedPrompt is the prompt the vendor actually used, when it says.
	RevisedPrompt string
}

// Result is what one call produced.
type Result struct {
	Images []Image
}

// ImageGenerator generates images with one configured model.
type ImageGenerator interface {
	// Generate runs one request. It fails before sending when the request
	// carries more reference images than MaxInputImages allows.
	Generate(ctx context.Context, req Request) (*Result, error)
	// MaxInputImages is how many reference images one request may carry;
	// 0 means the model only does text-to-image.
	MaxInputImages() int
	GetModelName() string
	GetModelID() string
}
