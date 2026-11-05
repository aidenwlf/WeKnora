package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type debugImageGenService struct {
	interfaces.ModelService
	generator *debugImageGenerator
}

func (s *debugImageGenService) GetModelByID(context.Context, string) (*types.Model, error) {
	return &types.Model{ID: "test", Type: types.ModelTypeImageGeneration}, nil
}

func (s *debugImageGenService) GetImageGenerationModel(context.Context, string) (imagegen.ImageGenerator, error) {
	return s.generator, nil
}

type debugImageGenerator struct {
	requests []imagegen.Request
	output   []byte
}

func (g *debugImageGenerator) Generate(_ context.Context, req imagegen.Request) (*imagegen.Result, error) {
	g.requests = append(g.requests, req)
	return &imagegen.Result{Images: []imagegen.Image{
		{Data: g.output, MIMEType: "image/png", RevisedPrompt: "a red circle"},
	}}, nil
}

func (g *debugImageGenerator) MaxInputImages() int  { return 4 }
func (g *debugImageGenerator) GetModelName() string { return "gpt-image-1" }
func (g *debugImageGenerator) GetModelID() string   { return "test" }

type debugUpload struct {
	name string
	data []byte
}

func runImageGenerationDebug(
	t *testing.T, gen *debugImageGenerator, prompt, options string, files ...debugUpload,
) (map[string]any, string, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	require.NoError(t, w.WriteField("input", prompt))
	require.NoError(t, w.WriteField("options", options))
	for _, file := range files {
		part, err := w.CreateFormFile("file", file.name)
		require.NoError(t, err)
		_, err = part.Write(file.data)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/models/test/debug", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: "test"}}
	(&ModelHandler{service: &debugImageGenService{generator: gen}}).DebugModel(c)
	if len(c.Errors) > 0 {
		return nil, "", c.Errors.Last().Error()
	}
	require.Equal(t, http.StatusOK, recorder.Code)
	var result struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	return result.Data, recorder.Body.String(), ""
}

func TestModelDebugImageGenerationSendsEveryUploadAsOriginal(t *testing.T) {
	first, second := debugPNG(t), append(debugPNG(t), 0)
	gen := &debugImageGenerator{output: debugPNG(t)}
	result, body, errText := runImageGenerationDebug(t, gen, "make it blue", `{"size":"1024x1024","count":2}`,
		debugUpload{"a.png", first}, debugUpload{"b.png", second})
	require.Empty(t, errText)

	require.Len(t, gen.requests, 1)
	sent := gen.requests[0]
	require.Equal(t, "make it blue", sent.Prompt)
	require.Equal(t, "1024x1024", sent.Size)
	require.Equal(t, 2, sent.Count)
	require.Len(t, sent.Images, 2)
	require.Equal(t, imagegen.InputImage{Data: first, MIMEType: "image/png", FileName: "a.png"}, sent.Images[0])
	require.Equal(t, imagegen.InputImage{Data: second, MIMEType: "image/png", FileName: "b.png"}, sent.Images[1])

	require.Equal(t, true, result["ok"])
	images := result["images"].([]any)
	require.Len(t, images, 1)
	image := images[0].(map[string]any)
	payload := base64.StdEncoding.EncodeToString(gen.output)
	require.Equal(t, "data:image/png;base64,"+payload, image["data_uri"])
	require.Equal(t, "a red circle", image["revised_prompt"])
	require.Equal(t, 1, strings.Count(body, payload), "only images[] carries the payload")
	raw := result["raw_response"].(map[string]any)["images"].([]any)[0].(map[string]any)
	require.Equal(t, float64(len(gen.output)), raw["bytes"])
	require.NotContains(t, raw, "data_uri")

	observations := result["observations"].(map[string]any)
	require.Equal(t, float64(1), observations["image_count"])
	require.Equal(t, float64(2), observations["input_image_count"])
}

func TestModelDebugImageGenerationRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, prompt string
		files        []debugUpload
		wantErr      string
	}{
		{"no prompt", " ", nil, "prompt cannot be empty"},
		{"not an image", "draw", []debugUpload{{"notes.txt", []byte("plain text")}}, "not an image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := &debugImageGenerator{}
			_, _, errText := runImageGenerationDebug(t, gen, tc.prompt, "", tc.files...)
			require.Contains(t, errText, tc.wantErr)
			require.Empty(t, gen.requests)
		})
	}
}
