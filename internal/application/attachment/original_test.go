package attachment

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

// Image editing needs the stored pixels, not the vision preview ReadImages
// prepares; authorization is the same session inventory check.
func TestReadOriginalImageReturnsStoredBytesAfterAuthorization(t *testing.T) {
	var big bytes.Buffer
	require.NoError(t, png.Encode(&big, image.NewRGBA(image.Rect(0, 0, 3000, 2000))))
	store, _ := imageFixture(t)
	stored := map[string][]byte{"local://owner/image": big.Bytes()}
	reader := NewReader(store, 0, func(_ context.Context, url string) ([]byte, error) {
		return stored[url], nil
	})

	// A standalone upload is its own image 1, addressed by its handle.
	got, err := reader.ReadOriginalImage(t.Context(), types.AttachmentHandle("document"), 1)
	require.NoError(t, err)
	require.Equal(t, big.Bytes(), got.Data)
	require.Equal(t, "image/png", got.MIMEType)
	require.Equal(t, "image-1.png", got.FileName)

	_, err = reader.ReadOriginalImage(t.Context(), types.AttachmentHandle("document"), 2)
	require.ErrorContains(t, err, "has 1 image")

	store.allowed = false
	_, err = reader.ReadOriginalImage(t.Context(), types.AttachmentHandle("document"), 1)
	require.ErrorIs(t, err, interfaces.ErrAttachmentNotFound)
}

func TestReadOriginalImageNumbersDocumentImages(t *testing.T) {
	store, data := imageFixture(t)
	refs, err := json.Marshal([]types.TemporaryDocumentImage{
		{URL: "local://owner/p1.jpg", MimeType: "image/jpeg"}, {URL: "local://owner/p2.png"},
	})
	require.NoError(t, err)
	store.doc.MimeType, store.doc.ImageRefs = "application/pdf", types.JSON(refs)
	var read string
	reader := NewReader(store, 0, func(_ context.Context, url string) ([]byte, error) {
		read = url
		return data, nil
	})
	got, err := reader.ReadOriginalImage(t.Context(), types.AttachmentHandle("document"), 2)
	require.NoError(t, err)
	require.Equal(t, "local://owner/p2.png", read)
	require.Equal(t, "image/png", got.MIMEType, "an unlabelled image is identified by its bytes")
}

// A model hands an upload's image back by the link its text shows; only the
// session's own uploads answer to it, and others are never loaded.
func TestReadOriginalImageByURLFindsOnlySessionUploads(t *testing.T) {
	store, data := imageFixture(t)
	var reads []string
	reader := NewReader(store, 0, func(_ context.Context, url string) ([]byte, error) {
		reads = append(reads, url)
		return data, nil
	})

	got, err := reader.ReadOriginalImageByURL(t.Context(), "local://owner/image")
	require.NoError(t, err)
	require.Equal(t, data, got.Data)
	require.Equal(t, "image/png", got.MIMEType)

	_, err = reader.ReadOriginalImageByURL(t.Context(), "local://owner/other")
	require.ErrorIs(t, err, interfaces.ErrAttachmentNotFound)

	store.allowed = false
	loads := store.loads
	_, err = reader.ReadOriginalImageByURL(t.Context(), "local://owner/image")
	require.ErrorIs(t, err, interfaces.ErrAttachmentNotFound)
	require.Equal(t, loads, store.loads, "an upload outside the session is never loaded")
	require.Equal(t, []string{"local://owner/image"}, reads)
}
