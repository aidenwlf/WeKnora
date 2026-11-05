package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
)

const maxReferenceImageBytes = 32 << 20

// agentGeneratedImages stores generate_image outputs for the session owner and
// decides which resource:// images the model may hand back. Tool arguments are
// model-controlled, so a reference is only honoured when this session already
// showed it: an output of this run, a live artifact of one of the session's
// answers, or an image the user sent in the session.
type agentGeneratedImages struct {
	db                 *gorm.DB
	catalog            interfaces.ResourceCatalog
	files              func(context.Context, string) (interfaces.FileService, error)
	tenantID           uint64
	ownerID, sessionID string
	mu                 sync.Mutex
	produced           map[string]bool
}

func (s *agentGeneratedImages) Save(ctx context.Context, data []byte, fileName string) (string, error) {
	fs, err := s.files(ctx, "")
	if err != nil {
		return "", err
	}
	ref, err := fs.SaveBytes(ctx, data, s.tenantID, "generated-images/"+fileName, false)
	if err != nil {
		return "", err
	}
	if _, ok := types.ParseResourcePath(ref); !ok {
		_ = fs.DeleteFile(ctx, ref)
		return "", fmt.Errorf("image storage did not return a resource reference")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.produced == nil {
		s.produced = make(map[string]bool)
	}
	s.produced[ref] = true
	return ref, nil
}

func (s *agentGeneratedImages) Read(ctx context.Context, ref string) (*imagegen.InputImage, error) {
	unavailable := fmt.Errorf("only images shown in this conversation can be used " +
		"(the user's uploads, or images generated here)")
	if _, ok := types.ParseResourcePath(ref); !ok {
		return nil, unavailable
	}
	resource, err := s.catalog.Resolve(ctx, ref)
	if err != nil || resource == nil || resource.TenantID != s.tenantID || !s.allowed(ctx, ref) {
		return nil, unavailable
	}
	fs, err := s.files(ctx, resource.StorageBackendID)
	if err != nil {
		return nil, err
	}
	reader, err := fs.GetFile(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("image is no longer available")
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, maxReferenceImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReferenceImageBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", maxReferenceImageBytes)
	}
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		mime = resource.MimeType
	}
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("reference is not an image")
	}
	return &imagegen.InputImage{Data: data, MIMEType: mime, FileName: referenceImageName(resource, mime)}, nil
}

func (s *agentGeneratedImages) allowed(ctx context.Context, ref string) bool {
	s.mu.Lock()
	produced := s.produced[ref]
	s.mu.Unlock()
	return produced || s.isSessionArtifact(ctx, ref) || s.isSessionUserImage(ctx, ref)
}

// isSessionArtifact follows agentArtifactFiles: a live file of an answer in
// this session, owned by this session's user.
func (s *agentGeneratedImages) isSessionArtifact(ctx context.Context, ref string) bool {
	var count int64
	err := s.sessionMessages(ctx, "assistant").
		Joins("JOIN message_artifacts ma ON ma.message_id = m.id AND ma.session_id = m.session_id").
		Where("ma.url = ? AND ma.deleted_at IS NULL", ref).
		Count(&count).Error
	return err == nil && count > 0
}

// isSessionUserImage scans the session's user messages; images are a JSON
// column whose query syntax differs between PostgreSQL and SQLite.
func (s *agentGeneratedImages) isSessionUserImage(ctx context.Context, ref string) bool {
	var rows []struct{ Images types.MessageImages }
	if err := s.sessionMessages(ctx, "user").Select("m.images").Find(&rows).Error; err != nil {
		return false
	}
	for _, row := range rows {
		for _, image := range row.Images {
			if image.URL == ref {
				return true
			}
		}
	}
	return false
}

// sessionMessages selects live messages of role in this session, owned by
// this tenant and user.
func (s *agentGeneratedImages) sessionMessages(ctx context.Context, role string) *gorm.DB {
	return s.db.WithContext(ctx).Table("messages AS m").
		Joins("JOIN sessions s ON s.id = m.session_id").
		Where("m.session_id = ? AND m.role = ? AND m.deleted_at IS NULL", s.sessionID, role).
		Where("s.tenant_id = ? AND s.user_id = ? AND s.deleted_at IS NULL", s.tenantID, s.ownerID)
}

func referenceImageName(resource *types.StoredResource, mime string) string {
	if name := path.Base(resource.OriginalName); path.Ext(name) != "" {
		return name
	}
	return "image." + strings.TrimPrefix(mime, "image/")
}
