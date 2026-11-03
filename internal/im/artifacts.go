package im

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
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
			Image:       isIMImageName(name),
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

// maxIMAnswerImages bounds how many images one answer can push to a chat.
const maxIMAnswerImages = 9

// maxIMImageBytes is the smallest native image limit across WeCom, DingTalk
// and WeChat (10 MiB for WeCom).
const maxIMImageBytes = 10 << 20

var (
	markdownImageRef = regexp.MustCompile(`!\[[^\]]*\]\(((?:resource|local)://[^)\s]+)\)`)
	htmlImageRef     = regexp.MustCompile(`(?i)<img[^>]+src=["']((?:resource|local)://[^"']+)["']`)
)

// sendAnswerImages delivers images embedded in the answer as native image
// messages. Knowledge-base images are resource:// handles the IM client
// cannot resolve, and a local-storage deployment has no public URL to rewrite
// them to, so uploading the bytes is the only way the user sees the image.
func (s *Service) sendAnswerImages(
	ctx context.Context,
	incoming *IncomingMessage,
	adapter Adapter,
	tenant *types.Tenant,
	answer string,
	skip map[string]bool,
) {
	sender, ok := adapter.(FileSender)
	if !ok || strings.TrimSpace(answer) == "" {
		return
	}

	refs := extractIMImageRefs(answer)
	sent := 0
	for _, ref := range refs {
		if sent >= maxIMAnswerImages {
			break
		}
		if skip[ref] {
			continue
		}
		skip[ref] = true

		data, name, contentType, err := s.readImageReference(ctx, tenant, ref)
		if err != nil {
			logger.Warnf(ctx, "[IM] Failed to read answer image %s: %v", ref, err)
			continue
		}
		if len(data) == 0 || len(data) > maxIMImageBytes {
			logger.Warnf(ctx, "[IM] Skipping answer image %s: %d bytes exceeds the image limit", ref, len(data))
			continue
		}
		if err := sender.SendFile(ctx, incoming, &OutgoingFile{
			FileName:    name,
			ContentType: contentType,
			Data:        data,
			Image:       true,
		}); err != nil {
			logger.Warnf(ctx, "[IM] Failed to send answer image %s on %s: %v", ref, adapter.Platform(), err)
			continue
		}
		sent++
		logger.Infof(ctx, "[IM] Answer image sent: platform=%s user=%s ref=%s size=%d",
			adapter.Platform(), incoming.UserID, ref, len(data))
	}
}

func extractIMImageRefs(answer string) []string {
	seen := make(map[string]bool)
	var refs []string
	add := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	for _, match := range markdownImageRef.FindAllStringSubmatch(answer, -1) {
		add(match[1])
	}
	for _, match := range htmlImageRef.FindAllStringSubmatch(answer, -1) {
		add(match[1])
	}
	return refs
}

func (s *Service) readImageReference(
	ctx context.Context,
	tenant *types.Tenant,
	reference string,
) ([]byte, string, string, error) {
	physical := reference
	var resource *types.StoredResource
	if s.resourceCatalog != nil {
		resolved, res, err := s.resourceCatalog.ResolvePath(ctx, reference)
		if err != nil {
			return nil, "", "", fmt.Errorf("resolve image path: %w", err)
		}
		physical = resolved
		resource = res
	}
	if tenant != nil {
		if resource != nil {
			if resource.TenantID != 0 && resource.TenantID != tenant.ID {
				return nil, "", "", fmt.Errorf("image belongs to another workspace")
			}
		} else if err := secutils.ValidateStoragePathTenant(physical, tenant.ID); err != nil {
			return nil, "", "", fmt.Errorf("image path is outside the workspace: %w", err)
		}
	}

	svc := newIMFileServiceResolver(tenant, s.defaultFileSvc, s.storageResolver).ResolveFileService(physical)
	if svc == nil {
		return nil, "", "", fmt.Errorf("image storage unavailable")
	}
	reader, err := svc.GetFile(ctx, physical)
	if err != nil {
		return nil, "", "", fmt.Errorf("read image: %w", err)
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxIMImageBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("read image body: %w", err)
	}
	if len(data) == 0 {
		return nil, "", "", fmt.Errorf("empty image")
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", "", fmt.Errorf("reference is %s, not an image", contentType)
	}

	name := ""
	if resource != nil {
		name = strings.TrimSpace(resource.OriginalName)
	}
	if name == "" {
		name = filepath.Base(physical)
	}
	if !isIMImageName(name) {
		sum := sha1.Sum([]byte(reference))
		name = "image-" + hex.EncodeToString(sum[:4]) + imageExtensionForMIME(contentType)
	}
	return data, sanitizeIMFileName(name), contentType, nil
}

func isIMImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

func imageExtensionForMIME(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ".png"
	}
}

func artifactURLSet(artifacts types.MessageArtifacts) map[string]bool {
	out := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		if artifact.URL != "" {
			out[artifact.URL] = true
		}
	}
	return out
}
