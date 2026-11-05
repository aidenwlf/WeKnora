package handler

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// modelDebugImage is one generated image as the debugger shows it.
type modelDebugImage struct {
	DataURI       string `json:"data_uri"`
	MIMEType      string `json:"mime_type"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// modelDebugImageSummary stands in for an image in raw_response, which must
// not repeat the base64 payload already carried by images[].
type modelDebugImageSummary struct {
	MIMEType      string `json:"mime_type"`
	Bytes         int    `json:"bytes"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

type modelDebugImageRawResponse struct {
	Images []modelDebugImageSummary `json:"images"`
}

// debugImageGeneration runs one generation with every uploaded `file` part
// as an original reference image.
func (h *ModelHandler) debugImageGeneration(
	c *gin.Context, id string, model *types.Model, prompt string, opts ModelDebugOptions, started time.Time,
) {
	if strings.TrimSpace(prompt) == "" {
		_ = c.Error(errors.NewBadRequestError("prompt cannot be empty"))
		return
	}
	inputs, err := readModelDebugInputImages(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	requestPreview := modelDebugRequestPreview(model, prompt, nil, opts, "", 0)
	requestPreview["input_images"] = modelDebugInputImagesPreview(inputs)
	observations := gin.H{"input_image_count": len(inputs), "image_count": 0}

	ctx := c.Request.Context()
	generator, err := h.service.GetImageGenerationModel(ctx, id)
	if err != nil {
		writeModelDebugResultWith(c, started, requestPreview, nil, err, observations, gin.H{"images": []modelDebugImage{}})
		return
	}
	result, err := generator.Generate(ctx, imagegen.Request{
		Prompt: prompt,
		Images: inputs,
		Size:   opts.Size,
		Count:  opts.Count,
	})
	images, raw := modelDebugImages(result)
	observations["image_count"] = len(images)
	var rawResponse any
	if result != nil {
		rawResponse = raw
	}
	writeModelDebugResultWith(c, started, requestPreview, rawResponse, err, observations, gin.H{"images": images})
}

// readModelDebugInputImages reads every multipart `file` part unchanged:
// editing works on the original pixels, so nothing is downscaled.
func readModelDebugInputImages(c *gin.Context) ([]imagegen.InputImage, error) {
	if c.Request.MultipartForm == nil {
		return nil, nil
	}
	headers := c.Request.MultipartForm.File["file"]
	inputs := make([]imagegen.InputImage, 0, len(headers))
	for _, header := range headers {
		input, err := readModelDebugInputImage(header)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func readModelDebugInputImage(header *multipart.FileHeader) (imagegen.InputImage, error) {
	maxFileBytes := secutils.GetMaxFileSize()
	tooLarge := errors.NewBadRequestError(fmt.Sprintf("file cannot exceed %d MB", secutils.GetMaxFileSizeMB()))
	if header.Size > maxFileBytes {
		return imagegen.InputImage{}, tooLarge
	}
	file, err := header.Open()
	if err != nil {
		return imagegen.InputImage{}, errors.NewBadRequestError("failed to read uploaded file")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return imagegen.InputImage{}, errors.NewBadRequestError("failed to read uploaded file")
	}
	if int64(len(data)) > maxFileBytes {
		return imagegen.InputImage{}, tooLarge
	}
	mimeType := modelDebugImageMIMEType(data, header.Header.Get("Content-Type"))
	if mimeType == "" {
		return imagegen.InputImage{}, errors.NewBadRequestError(
			fmt.Sprintf("file %q is not an image", header.Filename))
	}
	return imagegen.InputImage{Data: data, MIMEType: mimeType, FileName: header.Filename}, nil
}

// modelDebugImageMIMEType sniffs the bytes and falls back to the declared
// type only when sniffing is inconclusive (formats such as AVIF and HEIC that
// the sniffer does not know). "" means the upload is not an image.
func modelDebugImageMIMEType(data []byte, declared string) string {
	sniffed := http.DetectContentType(data)
	if strings.HasPrefix(sniffed, "image/") {
		return sniffed
	}
	declared = strings.ToLower(strings.TrimSpace(strings.SplitN(declared, ";", 2)[0]))
	if sniffed == "application/octet-stream" && strings.HasPrefix(declared, "image/") {
		return declared
	}
	return ""
}

func modelDebugInputImagesPreview(inputs []imagegen.InputImage) gin.H {
	files := make([]gin.H, 0, len(inputs))
	for _, input := range inputs {
		files = append(files, gin.H{"name": input.FileName, "size": len(input.Data), "mime_type": input.MIMEType})
	}
	return gin.H{"count": len(inputs), "files": files}
}

// modelDebugImages returns the generated images for display and the same
// list without its payload for raw_response.
func modelDebugImages(result *imagegen.Result) ([]modelDebugImage, modelDebugImageRawResponse) {
	images := []modelDebugImage{}
	raw := modelDebugImageRawResponse{Images: []modelDebugImageSummary{}}
	if result == nil {
		return images, raw
	}
	for _, img := range result.Images {
		mimeType := img.MIMEType
		if mimeType == "" {
			mimeType = http.DetectContentType(img.Data)
		}
		images = append(images, modelDebugImage{
			DataURI:       "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data),
			MIMEType:      mimeType,
			RevisedPrompt: img.RevisedPrompt,
		})
		raw.Images = append(raw.Images, modelDebugImageSummary{
			MIMEType: mimeType, Bytes: len(img.Data), RevisedPrompt: img.RevisedPrompt,
		})
	}
	return images, raw
}
