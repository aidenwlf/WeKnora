package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// Download fetches a file a vendor reply points at — a generated image the
// vendor hosts for a while — through the endpoint's SSRF-safe client, reading
// at most limit bytes. It sends none of the endpoint's credentials or
// headers: the URL is usually pre-signed object storage, not the vendor API.
func (e Endpoint) Download(ctx context.Context, url string, limit int64) ([]byte, string, error) {
	if err := secutils.ValidateURLForSSRF(url); err != nil {
		return nil, "", fmt.Errorf("download SSRF check failed: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "WeKnora")
	resp, err := e.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := ReadResponseCapped(resp.Body, limit)
	if err != nil {
		if errors.Is(err, ErrResponseTooLarge) {
			return nil, "", err
		}
		return nil, "", &TransportError{Op: "read download", Err: err}
	}
	return data, resp.Header.Get("Content-Type"), nil
}
