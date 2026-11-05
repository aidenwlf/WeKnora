// Package openaiimages implements OpenAI's Images API and the gateways that
// copied it: text-to-image is a JSON POST to {base}/images/generations and
// image-to-image a multipart POST to {base}/images/edits carrying the
// reference images, both answering data[] with b64_json or url.
//
// Optional fields are sent only when set. response_format in particular is
// rejected by the gpt-image models, which always answer b64_json, and needed
// by dall-e and some gateways that would otherwise answer a short-lived url;
// both reply shapes are decoded, so nothing is sent unless
// api.ImagesSettings names a format.
//
// https://developers.openai.com/api/reference/resources/images/methods/generate
// https://developers.openai.com/api/reference/resources/images/methods/edit
package openaiimages

import (
	"context"
	"fmt"
	"mime"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/api"
)

// Config is everything the client needs, already resolved.
type Config struct {
	Endpoint api.Endpoint
	Settings api.ImagesSettings
	Retry    api.RetryPolicy
}

// Client talks the OpenAI Images shape to one endpoint.
type Client struct {
	cfg Config
}

// New builds a client.
func New(cfg Config) *Client { return &Client{cfg: cfg} }

// GenerateImages sends one request: generations without reference images,
// edits with them.
func (c *Client) GenerateImages(ctx context.Context, req api.ImageGenerationRequest) ([]api.GeneratedImage, error) {
	var decoded response
	var err error
	if len(req.Images) == 0 {
		err = c.cfg.Endpoint.PostJSONWithRetry(
			ctx, c.generationsURL(), c.GenerationsBody(req), &decoded, c.cfg.Retry, "image generation")
	} else {
		err = c.postEdits(ctx, req, &decoded)
	}
	if err != nil {
		return nil, err
	}
	return c.decode(ctx, &decoded)
}

// GenerationsBody is the golden-test entry point: the JSON body of a
// text-to-image request.
func (c *Client) GenerationsBody(req api.ImageGenerationRequest) any {
	return generationsBody{
		Model:          c.cfg.Endpoint.Model,
		Prompt:         req.Prompt,
		N:              countField(req.Count),
		Size:           req.Size,
		ResponseFormat: c.cfg.Settings.ResponseFormat,
	}
}

// EditsForm is the golden-test entry point: the text parts and file parts of
// an image-to-image request, in the order they are written.
func (c *Client) EditsForm(req api.ImageGenerationRequest) ([]api.FormField, []api.FormFile) {
	fields := []api.FormField{
		{Name: "model", Value: c.cfg.Endpoint.Model},
		{Name: "prompt", Value: req.Prompt},
	}
	if n := countField(req.Count); n > 0 {
		fields = append(fields, api.FormField{Name: "n", Value: strconv.Itoa(n)})
	}
	if req.Size != "" {
		fields = append(fields, api.FormField{Name: "size", Value: req.Size})
	}
	if format := c.cfg.Settings.ResponseFormat; format != "" {
		fields = append(fields, api.FormField{Name: "response_format", Value: format})
	}
	// One image is `image`, several are `image[]`: the official SDKs'
	// convention, which form parsers behind the compatible servers follow.
	field := "image"
	if len(req.Images) > 1 {
		field = "image[]"
	}
	files := make([]api.FormFile, 0, len(req.Images))
	for i, img := range req.Images {
		files = append(files, api.FormFile{
			Field: field, FileName: fileName(img, i), ContentType: img.MIMEType, Data: img.Data,
		})
	}
	return fields, files
}

type generationsBody struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
}

func (c *Client) postEdits(ctx context.Context, req api.ImageGenerationRequest, out *response) error {
	url, err := c.editsURL()
	if err != nil {
		return err
	}
	fields, files := c.EditsForm(req)
	body, contentType, err := api.EncodeMultipartFiles(fields, files)
	if err != nil {
		return err
	}
	return c.cfg.Endpoint.PostMultipartWithRetry(ctx, url, body, contentType, out, c.cfg.Retry, "image edit")
}

func (c *Client) generationsURL() string {
	if c.cfg.Endpoint.URL != "" {
		return c.cfg.Endpoint.Resolve("")
	}
	return c.underBase(c.cfg.Settings.GenerationsPath)
}

// editsURL derives the edits URL from a vendor-computed one by swapping the
// final path: the vendor hook (Azure's) is asked for one URL per model type,
// and every vendor that computes one serves both operations side by side.
func (c *Client) editsURL() (string, error) {
	ep := c.cfg.Endpoint
	if ep.URL == "" {
		return c.underBase(c.cfg.Settings.EditsPath), nil
	}
	root, ok := strings.CutSuffix(ep.URL, c.cfg.Settings.GenerationsPath)
	if !ok {
		return "", fmt.Errorf("image edit: cannot derive the edits URL from %q", ep.URL)
	}
	ep.URL = root + c.cfg.Settings.EditsPath
	return ep.Resolve(""), nil
}

// underBase appends path to the base URL, first dropping either operation's
// path when the operator pasted a full endpoint as the base.
func (c *Client) underBase(path string) string {
	ep := c.cfg.Endpoint
	base := strings.TrimRight(ep.BaseURL, "/")
	base = strings.TrimSuffix(base, c.cfg.Settings.GenerationsPath)
	ep.BaseURL = strings.TrimSuffix(base, c.cfg.Settings.EditsPath)
	return ep.Resolve(path)
}

func countField(n int) int {
	if n > 1 {
		return n
	}
	return 0
}

// fileName keeps the caller's name when it has an extension, which is what
// servers identify the format by, and otherwise names the part after its
// media type.
func fileName(img api.ImageGenerationInput, i int) string {
	if strings.Contains(img.FileName, ".") {
		return img.FileName
	}
	ext := ".png"
	if exts, _ := mime.ExtensionsByType(img.MIMEType); len(exts) > 0 {
		ext = preferredExt(exts)
	}
	return fmt.Sprintf("image-%d%s", i+1, ext)
}

// preferredExt picks the common spelling where Go's table lists several
// (.jpe, .jpeg, .jpg).
func preferredExt(exts []string) string {
	for _, e := range exts {
		if e == ".jpg" || e == ".png" || e == ".webp" {
			return e
		}
	}
	return exts[0]
}
