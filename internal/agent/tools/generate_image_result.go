package tools

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
)

// generatedImageView is one entry of the result card's images list. The chat
// UI loads URL like any other resource:// image; bytes never travel in Data.
type generatedImageView struct {
	URL      string `json:"url"`
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	// FileSize feeds the message artifact derived from this result.
	FileSize int64 `json:"file_size,omitempty"`
}

// generateImageData is the persisted shape of a generate_image result. It is
// read back from stored steps, where Data has been through JSON.
type generateImageData struct {
	RevisedPrompt string               `json:"revised_prompt,omitempty"`
	Images        []generatedImageView `json:"images"`
}

// GeneratedImageArtifacts returns the images successful generate_image calls
// in steps produced, once per reference, as message artifacts.
func GeneratedImageArtifacts(steps []types.AgentStep) types.MessageArtifacts {
	var artifacts types.MessageArtifacts
	seen := make(map[string]bool)
	for _, step := range steps {
		for _, call := range step.ToolCalls {
			if call.Name != ToolGenerateImage || call.Result == nil || !call.Result.Success {
				continue
			}
			for _, image := range parseGenerateImageData(call.Result.Data).Images {
				if image.URL == "" || seen[image.URL] {
					continue
				}
				seen[image.URL] = true
				artifacts = append(artifacts, types.MessageArtifact{
					URL: image.URL, FileName: image.FileName, FileType: path.Ext(image.FileName),
					FileSize: image.FileSize, CreatedAt: step.Timestamp,
				})
			}
		}
	}
	return artifacts
}

func generateImageResult(req imagegen.Request, model string, saved []savedImage) *types.ToolResult {
	views := make([]generatedImageView, 0, len(saved))
	revised := ""
	for _, image := range saved {
		views = append(views, generatedImageView{
			URL: image.Ref, FileName: image.FileName, MIMEType: image.MIMEType,
			Width: image.Width, Height: image.Height, FileSize: image.Size,
		})
		if revised == "" {
			revised = image.RevisedPrompt
		}
	}
	data := map[string]interface{}{
		"display_type": GenerateImageDisplayType,
		"prompt":       req.Prompt,
		"model":        model,
		"input_count":  len(req.Images),
		"images":       views,
	}
	if revised != "" {
		data["revised_prompt"] = revised
	}
	if req.Size != "" {
		data["size"] = req.Size
	}
	return &types.ToolResult{Success: true, Output: generateImageSummary(data), Data: data}
}

// generateImageSummary is the model's view of a result, live and replayed:
// the references it may embed or edit, never the pixels.
func generateImageSummary(data map[string]interface{}) string {
	parsed := parseGenerateImageData(data)
	var b strings.Builder
	fmt.Fprintf(&b, "Generated %d image(s); they are already shown to the user.\n", len(parsed.Images))
	for i, image := range parsed.Images {
		fmt.Fprintf(&b, "%d. %s", i+1, image.URL)
		if image.Width > 0 && image.Height > 0 {
			fmt.Fprintf(&b, " (%dx%d)", image.Width, image.Height)
		}
		b.WriteString("\n")
	}
	if parsed.RevisedPrompt != "" {
		fmt.Fprintf(&b, "Prompt used by the model: %s\n", parsed.RevisedPrompt)
	}
	b.WriteString("Embed one as ![description](reference) only if the answer refers to it; " +
		"pass a reference in images to edit it further.")
	return b.String()
}

// parseGenerateImageData accepts live Data and Data decoded from storage.
func parseGenerateImageData(data map[string]interface{}) generateImageData {
	var parsed generateImageData
	if stringField(data, "display_type") != GenerateImageDisplayType {
		return parsed
	}
	raw, err := json.Marshal(data)
	if err == nil {
		_ = json.Unmarshal(raw, &parsed)
	}
	return parsed
}
