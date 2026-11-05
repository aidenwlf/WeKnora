package attachment

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ReadOriginalImage authorizes like ReadImages but returns image number's
// stored bytes unchanged: editing works on the pixels, not a vision preview.
// A standalone image upload is its own image 1.
func (r *Reader) ReadOriginalImage(ctx context.Context, ref string, number int) (*imagegen.InputImage, error) {
	if number < 1 {
		return nil, fmt.Errorf("image numbers start at 1")
	}
	inventory, err := r.inventory(ctx)
	if err != nil {
		return nil, err
	}
	item, err := findAttachment(inventory, ref)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", interfaces.ErrAttachmentNotFound, err)
	}
	images, err := r.originalImages(ctx, item.ID, number)
	if err != nil {
		return nil, err
	}
	if number > len(images) {
		return nil, fmt.Errorf("%s has %d image(s); image %d does not exist",
			types.AttachmentPath(item.ID), len(images), number)
	}
	return r.originalImage(ctx, item.ID, number, images[number-1])
}

// ReadOriginalImageByURL finds an image of one of the session's uploads by its
// stored reference. Attachment text shows those images as markdown links, so
// a model hands them back in that form instead of attachment://…/images/N.
func (r *Reader) ReadOriginalImageByURL(ctx context.Context, url string) (*imagegen.InputImage, error) {
	inventory, err := r.inventory(ctx)
	if err != nil {
		return nil, err
	}
	url = strings.TrimSpace(url)
	for _, item := range inventory {
		doc, err := r.store.TemporaryDocument(ctx, item.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load attachment: %w", err)
		}
		if doc == nil {
			continue
		}
		for i, image := range doc.ViewableImages() {
			if url != "" && image.URL == url {
				return r.originalImage(ctx, item.ID, i+1, image)
			}
		}
	}
	return nil, fmt.Errorf("%w: not an image of this conversation's uploads", interfaces.ErrAttachmentNotFound)
}

// originalImages waits for parsing only when the image is not known yet: an
// uploaded picture is readable before its OCR finishes.
func (r *Reader) originalImages(
	ctx context.Context, documentID string, number int,
) ([]types.TemporaryDocumentImage, error) {
	doc, err := r.store.TemporaryDocument(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load attachment: %w", err)
	}
	if doc != nil && (number <= len(doc.ViewableImages()) || !attachmentPending(doc.Status)) {
		return doc.ViewableImages(), nil
	}
	doc, err = r.awaitDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("attachment is no longer available")
	}
	if attachmentPending(doc.Status) && number > len(doc.ViewableImages()) {
		return nil, fmt.Errorf("attachment is still being processed; try again shortly")
	}
	return doc.ViewableImages(), nil
}

// originalImage reads one authorized image's stored bytes.
func (r *Reader) originalImage(
	ctx context.Context, documentID string, number int, image types.TemporaryDocumentImage,
) (*imagegen.InputImage, error) {
	if r.readImage == nil {
		return nil, fmt.Errorf("attachment image storage is unavailable")
	}
	data, err := r.readImage(ctx, image.URL)
	if err != nil {
		return nil, err
	}
	mime := image.MimeType
	if !strings.HasPrefix(mime, "image/") {
		mime = http.DetectContentType(data)
	}
	if !strings.HasPrefix(mime, "image/") {
		return nil, fmt.Errorf("image %d of %s is not an image", number, types.AttachmentPath(documentID))
	}
	return &imagegen.InputImage{Data: data, MIMEType: mime, FileName: originalImageName(number, mime)}, nil
}

// originalImageName gives the upload an extension servers recognise; the
// user's file name carries nothing the model needs.
func originalImageName(number int, mime string) string {
	ext := ".png"
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	return fmt.Sprintf("image-%d%s", number, ext)
}
