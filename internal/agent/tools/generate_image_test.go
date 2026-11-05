package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type fakeImageModel struct {
	maxInputs int
	requests  []imagegen.Request
	revised   string
}

func (m *fakeImageModel) Generate(_ context.Context, req imagegen.Request) (*imagegen.Result, error) {
	m.requests = append(m.requests, req)
	var result imagegen.Result
	for i := range max(req.Count, 1) {
		result.Images = append(result.Images, imagegen.Image{
			Data: testPNG(3+i, 2), MIMEType: "image/png", RevisedPrompt: m.revised,
		})
	}
	return &result, nil
}

func (m *fakeImageModel) MaxInputImages() int  { return m.maxInputs }
func (m *fakeImageModel) GetModelName() string { return "gpt-image-1" }
func (m *fakeImageModel) GetModelID() string   { return "model-1" }

type fakeImageStore struct {
	saved   map[string][]byte
	allowed map[string]bool
}

func (s *fakeImageStore) Save(_ context.Context, data []byte, _ string) (string, error) {
	ref := fmt.Sprintf("resource://%022d", len(s.saved)+1)
	s.saved[ref] = data
	return ref, nil
}

func (s *fakeImageStore) Read(_ context.Context, ref string) (*imagegen.InputImage, error) {
	if !s.allowed[ref] {
		return nil, fmt.Errorf("not in this conversation")
	}
	return &imagegen.InputImage{Data: []byte(ref), MIMEType: "image/png", FileName: "r.png"}, nil
}

type fakeAttachmentImages struct {
	reads   []string
	uploads map[string][]byte
}

func (a *fakeAttachmentImages) ReadOriginalImage(
	_ context.Context, ref string, number int,
) (*imagegen.InputImage, error) {
	a.reads = append(a.reads, fmt.Sprintf("%s#%d", ref, number))
	return &imagegen.InputImage{Data: []byte(ref), MIMEType: "image/jpeg", FileName: "a.jpg"}, nil
}

func (a *fakeAttachmentImages) ReadOriginalImageByURL(_ context.Context, url string) (*imagegen.InputImage, error) {
	data, ok := a.uploads[url]
	if !ok {
		return nil, fmt.Errorf("%w: %s", interfaces.ErrAttachmentNotFound, url)
	}
	return &imagegen.InputImage{Data: data, MIMEType: "image/png", FileName: "image-1.png"}, nil
}

func testPNG(w, h int) []byte {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

func newImageToolForTest(maxInputs int) (*GenerateImageTool, *fakeImageModel, *fakeImageStore) {
	model := &fakeImageModel{maxInputs: maxInputs, revised: "a red fox, watercolour"}
	store := &fakeImageStore{saved: map[string][]byte{}, allowed: map[string]bool{}}
	return NewGenerateImageTool(model, store), model, store
}

func runImageTool(t *testing.T, tool *GenerateImageTool, args string) *types.ToolResult {
	t.Helper()
	result, err := tool.Execute(t.Context(), json.RawMessage(args))
	require.NoError(t, err)
	return result
}

// The chat card and later turns read this exact shape; bytes never ride along.
func TestGenerateImageResultDataShape(t *testing.T) {
	tool, model, store := newImageToolForTest(0)
	result := runImageTool(t, tool, `{"prompt":"一只红色的狐狸","size":"1024X1024","count":2}`)
	require.True(t, result.Success, result.Error)
	require.Equal(t, "1024x1024", model.requests[0].Size)
	require.Empty(t, result.Images)

	raw, err := json.Marshal(result.Data)
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(raw, &data))
	images := data["images"].([]any)
	delete(data, "images")
	require.Equal(t, map[string]any{
		"display_type": "image_generation", "prompt": "一只红色的狐狸", "revised_prompt": "a red fox, watercolour",
		"model": "gpt-image-1", "size": "1024x1024", "input_count": float64(0),
	}, data)
	require.Len(t, images, 2)
	for i, item := range images {
		image := item.(map[string]any)
		ref := image["url"].(string)
		require.Contains(t, store.saved, ref)
		require.Regexp(t, `^[0-9a-f-]{36}\.png$`, image["file_name"])
		require.Equal(t, "image/png", image["mime_type"])
		require.Equal(t, float64(3+i), image["width"])
		require.Equal(t, float64(2), image["height"])
		require.Contains(t, result.Output, ref)
	}
	require.NotContains(t, string(raw), "base64")
	require.Contains(t, result.Output, "already shown to the user")
	require.Contains(t, result.Output, "a red fox, watercolour")
}

func TestGenerateImageRefusesReferencesTheModelCannotTake(t *testing.T) {
	tool, model, _ := newImageToolForTest(0)
	tool.WithAttachments(&fakeAttachmentImages{})
	result := runImageTool(t, tool, `{"prompt":"edit it","images":["attachment://att_1"]}`)
	require.False(t, result.Success)
	require.Contains(t, result.Error, "only generates images from text")

	tool, model, _ = newImageToolForTest(1)
	tool.WithAttachments(&fakeAttachmentImages{})
	result = runImageTool(t, tool, `{"prompt":"merge","images":["attachment://att_1","attachment://att_2"]}`)
	require.False(t, result.Success)
	require.Contains(t, result.Error, "at most 1")
	require.Empty(t, model.requests, "no generation is paid for a refused call")
}

func TestGenerateImageResolvesReferencesThroughAuthorizers(t *testing.T) {
	tool, model, store := newImageToolForTest(4)
	attachments := &fakeAttachmentImages{}
	tool.WithAttachments(attachments)
	shown := "resource://" + strings.Repeat("s", 22)
	store.allowed[shown] = true

	result := runImageTool(t, tool,
		`{"prompt":"combine","images":["attachment://att_1","attachment://att_2/images/3","`+shown+`"]}`)
	require.True(t, result.Success, result.Error)
	require.Equal(t, []string{"att_1#1", "att_2#3"}, attachments.reads)
	got := model.requests[0].Images
	require.Len(t, got, 3)
	require.Equal(t, []byte(shown), got[2].Data)
	require.Equal(t, 3, result.Data["input_count"])

	for _, ref := range []string{
		"resource://" + strings.Repeat("x", 22), "local://7/chat-images/a.png", "sandbox:out.png",
		"attachment://att_1/images/0",
	} {
		result := runImageTool(t, tool, `{"prompt":"edit","images":["`+ref+`"]}`)
		require.False(t, result.Success, ref)
	}
	require.Len(t, model.requests, 1, "a refused reference must not reach the image model")
}

func TestGenerateImageWithoutAttachmentReaderRefusesUploads(t *testing.T) {
	tool, model, _ := newImageToolForTest(2)
	result := runImageTool(t, tool, `{"prompt":"edit","images":["attachment://att_1"]}`)
	require.False(t, result.Success)
	require.Empty(t, model.requests)
}

// Attachment text shows an upload's image as a res:// link, so the model hands
// it back in that form. The session store does not know uploads; the
// attachment reader decides them, and anything else keeps the store's refusal.
func TestGenerateImageTakesUploadImagesByTheirLink(t *testing.T) {
	tool, model, _ := newImageToolForTest(2)
	upload := "resource://" + strings.Repeat("u", 22)
	tool.WithAttachments(&fakeAttachmentImages{uploads: map[string][]byte{upload: []byte("upload")}})

	result := runImageTool(t, tool, `{"prompt":"make it blue","images":["`+upload+`"]}`)
	require.True(t, result.Success, result.Error)
	require.Equal(t, []byte("upload"), model.requests[0].Images[0].Data)

	result = runImageTool(t, tool, `{"prompt":"edit","images":["resource://`+strings.Repeat("o", 22)+`"]}`)
	require.False(t, result.Success)
	require.Contains(t, result.Error, "not in this conversation")
	require.Len(t, model.requests, 1, "a refused reference must not reach the image model")
}

// A run that suspends on a prompt is completed twice: once suspended, once
// resumed from the snapshot. Both completions must name each image once.
func TestGeneratedImageArtifactsSurviveSuspensionOnce(t *testing.T) {
	tool, _, _ := newImageToolForTest(0)
	first := runImageTool(t, tool, `{"prompt":"cat"}`)
	failed := &types.ToolResult{Success: false, Error: "boom", Data: first.Data}
	beforePrompt := []types.AgentStep{{
		Timestamp: time.Now(),
		ToolCalls: []types.ToolCall{
			{Name: ToolGenerateImage, Result: first},
			{Name: ToolGenerateImage, Result: failed},
		},
	}}
	suspended := GeneratedImageArtifacts(SanitizeAgentStepsForStorage(beforePrompt))
	require.Len(t, suspended, 1)

	raw, err := json.Marshal(beforePrompt)
	require.NoError(t, err)
	var restored []types.AgentStep
	require.NoError(t, json.Unmarshal(raw, &restored))
	second := runImageTool(t, tool, `{"prompt":"dog"}`)
	resumed := append(restored, types.AgentStep{ToolCalls: []types.ToolCall{
		{Name: ToolGenerateImage, Result: second},
		{Name: ToolGenerateImage, Result: first},
	}})
	artifacts := GeneratedImageArtifacts(resumed)
	require.Len(t, artifacts, 2)
	require.Equal(t, suspended[0].URL, artifacts[0].URL)
	require.Equal(t, firstImageRef(first), artifacts[0].URL)
	require.Equal(t, ".png", artifacts[0].FileType)
	require.Positive(t, artifacts[0].FileSize)
}

// Replayed history keeps the references so a later turn can edit the image.
func TestGenerateImageHistoryKeepsReferences(t *testing.T) {
	tool, _, _ := newImageToolForTest(0)
	result := runImageTool(t, tool, `{"prompt":"cat"}`)
	stored := SanitizeAgentStepsForStorage([]types.AgentStep{{
		ToolCalls: []types.ToolCall{{Name: ToolGenerateImage, Result: result}},
	}})
	replayed := CompactToolOutputForHistory(ToolGenerateImage, stored[0].ToolCalls[0].Result)
	require.Equal(t, result.Output, replayed)
	require.Contains(t, replayed, firstImageRef(result))
}

func firstImageRef(result *types.ToolResult) string {
	return result.Data["images"].([]generatedImageView)[0].URL
}
