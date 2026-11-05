package openaiimages

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/api"
)

// maxDownloadBytes caps one image fetched from a reply url. A 4096x4096 PNG
// is well under it.
const maxDownloadBytes = 50 << 20

type response struct {
	Data []struct {
		B64JSON       string `json:"b64_json"`
		URL           string `json:"url"`
		RevisedPrompt string `json:"revised_prompt"`
		// Error is a per-image failure; Ark reports one image of a batch
		// this way while the others succeed.
		Error *vendorError `json:"error"`
	} `json:"data"`
	Error *vendorError `json:"error"`
}

type vendorError struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

func (e *vendorError) String() string {
	if e.Code != nil && e.Code != "" {
		return fmt.Sprintf("%v: %s", e.Code, e.Message)
	}
	return e.Message
}

// decode turns the reply into images, downloading url items. An error object
// or a reply without one usable image is an error: an empty result would
// look like a successful generation of nothing.
func (c *Client) decode(ctx context.Context, r *response) ([]api.GeneratedImage, error) {
	if r.Error != nil && (r.Error.Message != "" || r.Error.Code != nil) {
		return nil, fmt.Errorf("image generation API error: %s", r.Error)
	}
	var out []api.GeneratedImage
	var firstErr error
	for i, item := range r.Data {
		img, err := c.decodeItem(ctx, item.B64JSON, item.URL, item.Error)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("image %d: %w", i+1, err)
			}
			continue
		}
		img.RevisedPrompt = item.RevisedPrompt
		out = append(out, img)
	}
	if len(out) > 0 {
		return out, nil
	}
	if firstErr != nil {
		return nil, fmt.Errorf("image generation failed: %w", firstErr)
	}
	return nil, fmt.Errorf("image generation reply carries no images")
}

func (c *Client) decodeItem(ctx context.Context, b64, url string, vErr *vendorError) (api.GeneratedImage, error) {
	if vErr != nil && (vErr.Message != "" || vErr.Code != nil) {
		return api.GeneratedImage{}, fmt.Errorf("vendor error: %s", vErr)
	}
	var data []byte
	switch {
	case b64 != "":
		decoded, err := decodeBase64(b64)
		if err != nil {
			return api.GeneratedImage{}, err
		}
		data = decoded
	case url != "":
		downloaded, _, err := c.cfg.Endpoint.Download(ctx, url, maxDownloadBytes)
		if err != nil {
			return api.GeneratedImage{}, fmt.Errorf("download: %w", err)
		}
		data = downloaded
	default:
		return api.GeneratedImage{}, fmt.Errorf("neither b64_json nor url")
	}
	// The bytes decide, not a header: object storage often serves
	// application/octet-stream, and a gateway error page is text/html.
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return api.GeneratedImage{}, fmt.Errorf("not an image (%s)", mimeType)
	}
	return api.GeneratedImage{Data: data, MIMEType: mimeType}, nil
}

// decodeBase64 also accepts a data URI, which some gateways put in b64_json.
func decodeBase64(s string) ([]byte, error) {
	if strings.HasPrefix(s, "data:") {
		if _, payload, ok := strings.Cut(s, ","); ok {
			s = payload
		}
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("decode b64_json: %w", err)
	}
	return data, nil
}
