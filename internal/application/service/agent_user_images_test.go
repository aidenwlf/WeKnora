package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

const (
	userImageRef   = "resource://AbCdEfGhIjKlMnOpQrStUv"
	legacyImageRef = "minio://bucket/7/chat-images/old.png"
)

type userImageHistoryRepo struct {
	historyRepo
	current *types.Message
}

func (r *userImageHistoryRepo) GetMessage(_ context.Context, _, id string) (*types.Message, error) {
	if r.current != nil && r.current.ID == id {
		return r.current, nil
	}
	return nil, nil
}

func imageTurnRows() []*types.Message {
	rows := storedTurns(1)
	rows[0].Images = types.MessageImages{{URL: userImageRef}, {URL: legacyImageRef}}
	return rows
}

// Agents without generate_image must see exactly the prompt they saw before.
func TestHistoryNamesUserImagesOnlyWhenImageGenerationIsOffered(t *testing.T) {
	repo := &historyRepo{rows: imageTurnRows()}
	plain, _, err := LoadAgentHistory(t.Context(), repo, "s1", unlimitedBudget, false)
	require.NoError(t, err)
	require.Equal(t, []string{"question 1", "answer 1"}, contents(plain))

	named, _, err := LoadAgentHistory(t.Context(), repo, "s1", unlimitedBudget, false, WithUserImageReferences())
	require.NoError(t, err)
	require.Equal(t, "question 1\n\n[用户上传的图片，可作为 generate_image 的参考图]\n1. "+userImageRef,
		named[0].Content)
	require.NotContains(t, named[0].Content, legacyImageRef, "only resource:// images are accepted by the tool")
	require.Equal(t, "answer 1", named[1].Content)
}

func TestCurrentTurnImagesAreNamedFromTheStoredUserMessage(t *testing.T) {
	current := &types.Message{ID: "u-now", Role: "user", Images: types.MessageImages{{URL: userImageRef}}}
	svc := &sessionService{messageRepo: &userImageHistoryRepo{current: current}}
	req := &types.QARequest{
		Session: &types.Session{ID: "s1"}, UserMessageID: "u-now", ImageURLs: []string{"data:image/png;base64,AA=="},
	}
	require.Contains(t, svc.currentTurnImageReferences(t.Context(), req), "1. "+userImageRef)

	req.ImageURLs = nil
	require.Empty(t, svc.currentTurnImageReferences(t.Context(), req), "a turn without images reads nothing")
}

func TestImageGenerationIsOfferedOnlyWithToolAndModel(t *testing.T) {
	allowed := []string{tools.ToolGenerateImage}
	require.True(t, offersImageGeneration(&types.AgentConfig{AllowedTools: allowed, ImageGenerationModelID: "m"}))
	require.False(t, offersImageGeneration(&types.AgentConfig{AllowedTools: allowed}))
	require.False(t, offersImageGeneration(&types.AgentConfig{ImageGenerationModelID: "m"}),
		"generate_image is not a default tool")
}
