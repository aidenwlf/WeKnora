package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Inline chat images reach the model as pixels or a caption, never as a name,
// so without these lines the model cannot pass them to generate_image. Only
// runs that offer the tool get them; every other prompt stays byte-identical.

// AgentHistoryOption adjusts how LoadAgentHistory replays stored turns.
type AgentHistoryOption func(*historyReplay)

// WithUserImageReferences appends each stored user image's resource://
// reference to its turn; the model sees it as a res:// handle.
func WithUserImageReferences() AgentHistoryOption {
	return func(r *historyReplay) { r.userImageRefs = true }
}

// offersImageGeneration mirrors registerTools: the tool is allowed and the
// agent names an image model. A model that then fails to load only leaves
// unused reference lines behind.
func offersImageGeneration(config *types.AgentConfig) bool {
	return config != nil && config.ImageGenerationModelID != "" &&
		slices.Contains(tools.EffectiveAllowedTools(config.AllowedTools), tools.ToolGenerateImage)
}

// currentTurnImageReferences names the images stored on this turn's user
// message, which the request only carries as data URIs.
func (s *sessionService) currentTurnImageReferences(ctx context.Context, req *types.QARequest) string {
	if len(req.ImageURLs) == 0 || req.UserMessageID == "" || s.messageRepo == nil {
		return ""
	}
	msg, err := s.messageRepo.GetMessage(ctx, req.Session.ID, req.UserMessageID)
	if err != nil || msg == nil {
		logger.Warnf(ctx, "generate_image: cannot name this turn's images: %v", err)
		return ""
	}
	return userImageReferences(msg.Images)
}

// userImageReferences lists stored images by reference. Legacy provider paths
// are left out: generate_image only accepts resource:// images.
func userImageReferences(images types.MessageImages) string {
	var b strings.Builder
	n := 0
	for _, image := range images {
		if _, ok := types.ParseResourcePath(image.URL); !ok {
			continue
		}
		if n == 0 {
			b.WriteString("\n\n[用户上传的图片，可作为 generate_image 的参考图]\n")
		}
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, image.URL)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
