package im

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// maxIMOutboundFileBytes is the smallest native file limit across the
// supported platforms (DingTalk and WeCom temporary media both cap at 20 MB).
// Larger artifacts stay available through the web artifact API instead of
// failing halfway through an upload.
const maxIMOutboundFileBytes = 20 << 20

// sendArtifacts delivers each live artifact from the completed turn as a
// native file message. Adapters that do not implement FileSender are skipped:
// the textual answer already carries the artifact references, and silently
// dropping the files is preferable to failing the reply.
func (s *Service) sendArtifacts(
	ctx context.Context,
	incoming *IncomingMessage,
	adapter Adapter,
	tenant *types.Tenant,
	artifacts types.MessageArtifacts,
) {
	if s == nil || incoming == nil || adapter == nil || len(artifacts) == 0 {
		return
	}
	sender, ok := adapter.(FileSender)
	if !ok {
		return
	}

	for _, artifact := range artifacts {
		if artifact.Deleted() || strings.TrimSpace(artifact.URL) == "" {
			continue
		}
		if artifact.FileSize > maxIMOutboundFileBytes {
			logger.Warnf(ctx, "[IM] Skipping artifact %q: %d bytes exceeds the %d-byte IM limit",
				artifact.FileName, artifact.FileSize, maxIMOutboundFileBytes)
			continue
		}

		data, err := s.readArtifact(ctx, tenant, artifact)
		if err != nil {
			logger.Warnf(ctx, "[IM] Failed to read artifact %q: %v", artifact.FileName, err)
			continue
		}
		if len(data) == 0 {
			logger.Warnf(ctx, "[IM] Skipping empty artifact %q", artifact.FileName)
			continue
		}
		if len(data) > maxIMOutboundFileBytes {
			logger.Warnf(ctx, "[IM] Skipping artifact %q: payload exceeds the %d-byte IM limit",
				artifact.FileName, maxIMOutboundFileBytes)
			continue
		}

		name := sanitizeIMFileName(artifact.FileName)
		file := &OutgoingFile{
			FileName:    name,
			ContentType: imContentType(name),
			Data:        data,
		}
		if err := sender.SendFile(ctx, incoming, file); err != nil {
			logger.Warnf(ctx, "[IM] Failed to send artifact %q on %s: %v", name, adapter.Platform(), err)
			continue
		}
		logger.Infof(ctx, "[IM] Artifact sent: platform=%s user=%s name=%s size=%d",
			adapter.Platform(), incoming.UserID, name, len(data))
	}
}

// readArtifact resolves the artifact's physical locator and loads its bytes.
// The artifact URL comes from the assistant message we just persisted, not from
// request input; the tenant checks below still prevent a malformed row from
// crossing workspaces.
func (s *Service) readArtifact(
	ctx context.Context,
	tenant *types.Tenant,
	artifact types.MessageArtifact,
) ([]byte, error) {
	reference := strings.TrimSpace(artifact.URL)
	if reference == "" {
		return nil, fmt.Errorf("artifact has no storage path")
	}

	physical := reference
	var resource *types.StoredResource
	if s.resourceCatalog != nil {
		resolved, res, err := s.resourceCatalog.ResolvePath(ctx, reference)
		if err != nil {
			return nil, fmt.Errorf("resolve artifact path: %w", err)
		}
		physical = resolved
		resource = res
	}

	if tenant != nil {
		if resource != nil {
			if resource.TenantID != 0 && resource.TenantID != tenant.ID {
				return nil, fmt.Errorf("artifact belongs to another workspace")
			}
		} else if err := secutils.ValidateStoragePathTenant(physical, tenant.ID); err != nil {
			return nil, fmt.Errorf("artifact path is outside the workspace: %w", err)
		}
	}

	svc := newIMFileServiceResolver(tenant, s.defaultFileSvc, s.storageResolver).ResolveFileService(physical)
	if svc == nil {
		return nil, fmt.Errorf("artifact storage unavailable")
	}
	reader, err := svc.GetFile(ctx, physical)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(io.LimitReader(reader, maxIMOutboundFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read artifact body: %w", err)
	}
	return data, nil
}

func artifactsOf(msg *types.Message) types.MessageArtifacts {
	if msg == nil {
		return nil
	}
	return msg.Artifacts
}

func sanitizeIMFileName(name string) string {
	name = strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	name = strings.Trim(name, ".")
	if name == "" || name == string(filepath.Separator) {
		return "artifact"
	}
	return name
}

func imContentType(name string) string {
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); contentType != "" {
		return contentType
	}
	return "application/octet-stream"
}
