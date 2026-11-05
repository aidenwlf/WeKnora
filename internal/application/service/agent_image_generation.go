package service

import (
	"context"
	"fmt"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/attachment"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// newGenerateImageTool builds generate_image when the agent's image model
// resolves. Like the VLM lookup, the model resolves under the request context,
// which carries the agent's tenant for shared agents; generated files belong
// to the session owner. Nil means the tool is not offered this run.
func (s *agentService) newGenerateImageTool(
	ctx context.Context, config *types.AgentConfig, sessionID string,
) *tools.GenerateImageTool {
	if config.ImageGenerationModelID == "" {
		logger.Warnf(ctx, "generate_image not registered: the agent has no image generation model")
		return nil
	}
	generator, err := s.modelService.GetImageGenerationModel(ctx, config.ImageGenerationModelID)
	if err != nil {
		logger.Warnf(ctx, "generate_image not registered: load image generation model %s: %v",
			config.ImageGenerationModelID, err)
		return nil
	}
	store, err := s.generatedImageStore(ctx, sessionID)
	if err != nil {
		logger.Warnf(ctx, "generate_image not registered: %v", err)
		return nil
	}
	return tools.NewGenerateImageTool(generator, store)
}

func (s *agentService) generatedImageStore(ctx context.Context, sessionID string) (*agentGeneratedImages, error) {
	tenantID, ok := types.SandboxTenantIDFromContext(ctx)
	if !ok || tenantID == 0 || s.db == nil || sessionID == "" {
		return nil, fmt.Errorf("no session storage for generated images")
	}
	return &agentGeneratedImages{
		db: s.db, catalog: NewResourceCatalog(repository.NewResourceRepository(s.db)),
		tenantID: tenantID, sessionID: sessionID,
		ownerID: types.SessionOwnerIDFromContext(context.WithValue(ctx, types.TenantIDContextKey, tenantID)),
		files: func(ctx context.Context, backendID string) (interfaces.FileService, error) {
			if s.storageResolver != nil {
				fs, _, err := s.storageResolver.ResolveFileService(ctx, &types.Tenant{ID: tenantID}, backendID, "", "")
				if err == nil && fs == nil {
					err = fmt.Errorf("image storage is unavailable")
				}
				return fs, err
			}
			if s.fileService == nil {
				return nil, fmt.Errorf("image storage is unavailable")
			}
			return s.fileService, nil
		},
	}, nil
}

// attachGenerateImageAttachments lets generate_image read uploaded images.
// The attachment reader only exists after registerTools has run.
func attachGenerateImageAttachments(registry *tools.ToolRegistry, reader *attachment.Reader) {
	if reader == nil {
		return
	}
	if raw, err := registry.GetTool(tools.ToolGenerateImage); err == nil {
		if tool, ok := raw.(*tools.GenerateImageTool); ok {
			tool.WithAttachments(reader)
		}
	}
}
