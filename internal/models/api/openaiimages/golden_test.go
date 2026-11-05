package openaiimages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngBytes carries the PNG signature, which is all content sniffing reads.
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

type filePart struct {
	field, name, contentType string
	data                     []byte
}

type captured struct {
	path   string
	query  string
	auth   string
	apiKey string
	json   map[string]any
	fields map[string]string
	files  []filePart
}

// serve answers POSTs with reply and GET /files/a.png with an image, so url
// replies can point back at the same server.
func serve(t *testing.T, status int, reply string) (*httptest.Server, *captured) {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	got := &captured{fields: map[string]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			assert.Empty(t, r.Header.Get("Authorization"), "a download must not carry the API key")
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pngBytes)
			return
		}
		got.path, got.query = r.URL.Path, r.URL.RawQuery
		got.auth, got.apiKey = r.Header.Get("Authorization"), r.Header.Get("api-key")
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		if mediaType == "application/json" {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&got.json))
		} else {
			readForm(t, multipart.NewReader(r.Body, params["boundary"]), got)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(strings.ReplaceAll(reply, "{server}", "http://"+r.Host)))
	}))
	t.Cleanup(server.Close)
	return server, got
}

func readForm(t *testing.T, reader *multipart.Reader, got *captured) {
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return
		}
		require.NoError(t, err)
		data, _ := io.ReadAll(part)
		if part.FileName() != "" {
			got.files = append(got.files, filePart{
				field: part.FormName(), name: part.FileName(),
				contentType: part.Header.Get("Content-Type"), data: data,
			})
			continue
		}
		got.fields[part.FormName()] = string(data)
	}
}

func newClient(base, model string, settings api.ImagesSettings) *Client {
	defaults := api.DefaultImages()
	settings.GenerationsPath, settings.EditsPath = defaults.GenerationsPath, defaults.EditsPath
	return New(Config{
		Endpoint: api.Endpoint{BaseURL: base, Model: model, Auth: api.BearerAuth("k")},
		Settings: settings,
	})
}

func b64Reply(revised string) string {
	return `{"created": 1713833628, "data": [{"b64_json": "` + base64.StdEncoding.EncodeToString(pngBytes) +
		`", "revised_prompt": "` + revised + `"}], "usage": {"total_tokens": 100}}`
}

// gpt-image rejects response_format, so the baseline body is model and
// prompt, with n and size only when asked for.
func TestGenerationsBodyCarriesOnlyWhatIsSet(t *testing.T) {
	server, got := serve(t, http.StatusOK, b64Reply("a red circle, flat"))
	out, err := newClient(server.URL+"/v1", "gpt-image-1", api.ImagesSettings{}).
		GenerateImages(context.Background(), api.ImageGenerationRequest{Prompt: "a red circle"})
	require.NoError(t, err)

	assert.Equal(t, "/v1/images/generations", got.path)
	assert.Equal(t, "Bearer k", got.auth)
	assert.Equal(t, map[string]any{"model": "gpt-image-1", "prompt": "a red circle"}, got.json)
	require.Len(t, out, 1)
	assert.Equal(t, api.GeneratedImage{Data: pngBytes, MIMEType: "image/png", RevisedPrompt: "a red circle, flat"}, out[0])
}

func TestGenerationsBodyWithCountSizeAndFormat(t *testing.T) {
	server, got := serve(t, http.StatusOK, b64Reply(""))
	_, err := newClient(server.URL, "dall-e-2", api.ImagesSettings{ResponseFormat: "b64_json"}).
		GenerateImages(context.Background(), api.ImageGenerationRequest{Prompt: "p", Count: 2, Size: "256x256"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"model": "dall-e-2", "prompt": "p", "n": float64(2), "size": "256x256", "response_format": "b64_json",
	}, got.json)
}

// One reference image is sent as `image`, several as `image[]`, each with
// an extension-bearing filename and its own image Content-Type: OpenAI
// rejects image parts that arrive as application/octet-stream.
func TestEditsSendsTypedImageParts(t *testing.T) {
	jpeg := api.ImageGenerationInput{Data: []byte("jpeg"), MIMEType: "image/jpeg", FileName: "cat.jpg"}
	png := api.ImageGenerationInput{Data: pngBytes, MIMEType: "image/png"}
	for _, tc := range []struct {
		name   string
		images []api.ImageGenerationInput
		want   []filePart
	}{
		{"one image", []api.ImageGenerationInput{jpeg}, []filePart{
			{field: "image", name: "cat.jpg", contentType: "image/jpeg", data: []byte("jpeg")},
		}},
		{"several images", []api.ImageGenerationInput{jpeg, png}, []filePart{
			{field: "image[]", name: "cat.jpg", contentType: "image/jpeg", data: []byte("jpeg")},
			{field: "image[]", name: "image-2.png", contentType: "image/png", data: pngBytes},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, got := serve(t, http.StatusOK, b64Reply(""))
			_, err := newClient(server.URL+"/v1", "gpt-image-1", api.ImagesSettings{}).
				GenerateImages(context.Background(), api.ImageGenerationRequest{
					Prompt: "put them together", Images: tc.images, Count: 3, Size: "1024x1024",
				})
			require.NoError(t, err)
			assert.Equal(t, "/v1/images/edits", got.path)
			assert.Equal(t, map[string]string{
				"model": "gpt-image-1", "prompt": "put them together", "n": "3", "size": "1024x1024",
			}, got.fields)
			assert.Equal(t, tc.want, got.files)
		})
	}
}

// dall-e answers a url by default; the image is fetched without the API key
// and identified by its bytes, not the storage's octet-stream header.
func TestURLReplyIsDownloaded(t *testing.T) {
	server, _ := serve(t, http.StatusOK,
		`{"created": 1, "data": [{"url": "{server}/files/a.png", "revised_prompt": "r"}]}`)
	out, err := newClient(server.URL, "dall-e-3", api.ImagesSettings{}).
		GenerateImages(context.Background(), api.ImageGenerationRequest{Prompt: "p"})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, api.GeneratedImage{Data: pngBytes, MIMEType: "image/png", RevisedPrompt: "r"}, out[0])
}

func TestRepliesWithoutImagesAreErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		reply  string
		want   string
	}{
		"vendor error status": {http.StatusBadRequest,
			`{"error": {"message": "Your request was rejected by the safety system.", "code": "moderation_blocked"}}`,
			"safety system"},
		// Zhipu's envelope.
		"error object in a 2xx": {http.StatusOK, `{"error": {"code": "1301", "message": "unsafe content"}}`,
			"unsafe content"},
		"empty data": {http.StatusOK, `{"created": 1, "data": []}`, "carries no images"},
		// Ark reports a failed image of a batch inside data.
		"every item failed": {http.StatusOK,
			`{"data": [{"error": {"code": "OutputImageSensitiveContentDetected", "message": "blocked"}}]}`,
			"blocked"},
		"not an image": {http.StatusOK,
			`{"data": [{"b64_json": "` + base64.StdEncoding.EncodeToString([]byte("<html>")) + `"}]}`,
			"not an image"},
	} {
		t.Run(name, func(t *testing.T) {
			server, _ := serve(t, tc.status, tc.reply)
			_, err := newClient(server.URL, "m", api.ImagesSettings{}).
				GenerateImages(context.Background(), api.ImageGenerationRequest{Prompt: "p"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// One failed image of a batch does not discard the others.
func TestPartialBatchKeepsTheGoodImages(t *testing.T) {
	server, _ := serve(t, http.StatusOK, `{"data": [{"error": {"message": "blocked"}}, {"b64_json": "`+
		base64.StdEncoding.EncodeToString(pngBytes)+`"}]}`)
	out, err := newClient(server.URL, "m", api.ImagesSettings{}).
		GenerateImages(context.Background(), api.ImageGenerationRequest{Prompt: "p", Count: 2})
	require.NoError(t, err)
	assert.Len(t, out, 1)
}

// A vendor-computed URL (Azure's deployments path) keeps its query, and the
// edits URL is its sibling.
func TestVendorURLRoutesBothOperations(t *testing.T) {
	server, got := serve(t, http.StatusOK, b64Reply(""))
	settings := api.DefaultImages()
	client := New(Config{Settings: settings, Endpoint: api.Endpoint{
		BaseURL: server.URL, Model: "my-deployment", Auth: api.HeaderAuth("api-key", "k"),
		URL:   server.URL + "/openai/deployments/my-deployment/images/generations",
		Query: map[string]string{"api-version": "2025-04-01-preview"},
	}})
	ctx := context.Background()
	_, err := client.GenerateImages(ctx, api.ImageGenerationRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, "/openai/deployments/my-deployment/images/generations", got.path)
	assert.Equal(t, "api-version=2025-04-01-preview", got.query)
	assert.Equal(t, "k", got.apiKey)

	_, err = client.GenerateImages(ctx, api.ImageGenerationRequest{
		Prompt: "p", Images: []api.ImageGenerationInput{{Data: pngBytes, MIMEType: "image/png"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "/openai/deployments/my-deployment/images/edits", got.path)
	assert.Equal(t, "api-version=2025-04-01-preview", got.query)
}

func TestDoesNotDoubleThePath(t *testing.T) {
	server, got := serve(t, http.StatusOK, b64Reply(""))
	_, err := newClient(server.URL+"/v1/images/generations", "m", api.ImagesSettings{}).
		GenerateImages(context.Background(), api.ImageGenerationRequest{Prompt: "p"})
	require.NoError(t, err)
	assert.Equal(t, "/v1/images/generations", got.path)
}
