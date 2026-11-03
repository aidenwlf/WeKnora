package im

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type artifactFileService struct {
	interfaces.FileService
	data map[string][]byte
}

func (f *artifactFileService) GetFile(_ context.Context, path string) (io.ReadCloser, error) {
	data, ok := f.data[path]
	if !ok {
		return nil, io.EOF
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type artifactStorageResolver struct {
	svc interfaces.FileService
}

func (r *artifactStorageResolver) ResolveFileService(context.Context, *types.Tenant, string, string, string) (interfaces.FileService, string, error) {
	return r.svc, "", nil
}

func (r *artifactStorageResolver) ResolveBackend(context.Context, *types.Tenant, string, string) (*types.StorageBackend, error) {
	return nil, nil
}

type artifactSendingAdapter struct {
	Adapter
	files []*OutgoingFile
}

func (a *artifactSendingAdapter) Platform() Platform { return PlatformDingtalk }

func (a *artifactSendingAdapter) SendFile(_ context.Context, _ *IncomingMessage, file *OutgoingFile) error {
	a.files = append(a.files, file)
	return nil
}

type plainAdapter struct{ Adapter }

func (plainAdapter) Platform() Platform { return PlatformTelegram }

func TestSendArtifactsLoadsLiveFilesAndSkipsUnsendable(t *testing.T) {
	fileSvc := &artifactFileService{data: map[string][]byte{
		"local://tenant/1/report.docx": []byte("docx bytes"),
		"local://tenant/1/big.pdf":     bytes.Repeat([]byte("x"), maxIMOutboundFileBytes+1),
	}}
	svc := &Service{
		defaultFileSvc:  fileSvc,
		storageResolver: &artifactStorageResolver{svc: fileSvc},
	}
	adapter := &artifactSendingAdapter{}
	msg := &IncomingMessage{Platform: PlatformDingtalk, UserID: "u1"}
	tenant := &types.Tenant{ID: 1}

	svc.sendArtifacts(context.Background(), msg, adapter, tenant, types.MessageArtifacts{
		{URL: "local://tenant/1/report.docx", FileName: "report.docx", FileSize: 9},
		{URL: "local://tenant/1/deleted.pdf", FileName: "deleted.pdf", DeletedAt: timePtr()},
		{URL: "local://tenant/1/big.pdf", FileName: "big.pdf", FileSize: maxIMOutboundFileBytes + 1},
	})

	if len(adapter.files) != 1 {
		t.Fatalf("sent files = %d, want 1", len(adapter.files))
	}
	if got := adapter.files[0]; got.FileName != "report.docx" || string(got.Data) != "docx bytes" {
		t.Fatalf("unexpected file: %+v", got)
	}
	if adapter.files[0].ContentType == "" {
		t.Fatal("content type was not populated")
	}
}

func TestSendArtifactsWithoutFileSenderIsNoop(t *testing.T) {
	svc := &Service{defaultFileSvc: &artifactFileService{}}
	svc.sendArtifacts(context.Background(), &IncomingMessage{UserID: "u"}, plainAdapter{}, nil, types.MessageArtifacts{
		{URL: "local://tenant/1/report.docx", FileName: "report.docx"},
	})
}

func timePtr() *time.Time {
	now := time.Now()
	return &now
}

type imageCatalog struct {
	interfaces.ResourceCatalog
	resource *types.StoredResource
}

func (c imageCatalog) ResolvePath(context.Context, string) (string, *types.StoredResource, error) {
	return c.resource.PhysicalPath, c.resource, nil
}

func TestSendAnswerImagesUploadsResourceImages(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	fileSvc := &artifactFileService{data: map[string][]byte{
		"local://tenant/1/chart.png": png,
	}}
	svc := &Service{
		defaultFileSvc:  fileSvc,
		storageResolver: &artifactStorageResolver{svc: fileSvc},
		resourceCatalog: imageCatalog{resource: &types.StoredResource{
			TenantID: 1, PhysicalPath: "local://tenant/1/chart.png", OriginalName: "chart.png",
		}},
	}
	adapter := &artifactSendingAdapter{}
	answer := "see ![chart](resource://abc) and again ![chart](resource://abc)"
	svc.sendAnswerImages(context.Background(), &IncomingMessage{UserID: "u1"}, adapter, &types.Tenant{ID: 1}, answer, map[string]bool{})
	if len(adapter.files) != 1 {
		t.Fatalf("sent images = %d, want 1", len(adapter.files))
	}
	file := adapter.files[0]
	if !file.Image || file.FileName != "chart.png" || !strings.HasPrefix(file.ContentType, "image/") {
		t.Fatalf("unexpected image: %+v", file)
	}
}
