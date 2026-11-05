package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // Register GIF for generated image dimensions.
	_ "image/jpeg" // Register JPEG for generated image dimensions.
	_ "image/png"  // Register PNG for generated image dimensions.
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/models/imagegen"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp" // Register WebP for generated image dimensions.
)

var imageSizePattern = regexp.MustCompile(`^([1-9][0-9]{1,4})[xX×*]([1-9][0-9]{1,4})$`)

// savedImage is one generated image after storage.
type savedImage struct {
	Ref, FileName, MIMEType, RevisedPrompt string
	Width, Height                          int
	Size                                   int64
}

// loadReference resolves one model-supplied reference to original bytes.
// Every form goes through a session-scoped authorizer; raw storage paths and
// other sessions' files are refused there or here.
func (t *GenerateImageTool) loadReference(ctx context.Context, ref string) (*imagegen.InputImage, error) {
	switch {
	case strings.HasPrefix(ref, types.AttachmentPathScheme):
		if t.attachments == nil {
			return nil, fmt.Errorf("this conversation has no uploaded files")
		}
		handle, number, err := parseAttachmentImageRef(ref)
		if err != nil {
			return nil, err
		}
		return t.attachments.ReadOriginalImage(ctx, handle, number)
	case strings.HasPrefix(ref, types.ResourceScheme):
		return t.loadResource(ctx, ref)
	default:
		return nil, fmt.Errorf("unsupported reference; use attachment://att_xxxx, " +
			"attachment://att_xxxx/images/N, or an image reference (res://NNNN) shown in this conversation")
	}
}

// loadResource asks the session store first; an image inside one of the
// session's uploads is shown by the same kind of link, so the attachment
// reader decides those.
func (t *GenerateImageTool) loadResource(ctx context.Context, ref string) (*imagegen.InputImage, error) {
	if t.store == nil {
		return nil, fmt.Errorf("image storage is unavailable")
	}
	img, err := t.store.Read(ctx, ref)
	if err == nil || t.attachments == nil {
		return img, err
	}
	upload, uploadErr := t.attachments.ReadOriginalImageByURL(ctx, ref)
	switch {
	case uploadErr == nil:
		return upload, nil
	case errors.Is(uploadErr, interfaces.ErrAttachmentNotFound):
		return nil, err
	default:
		return nil, uploadErr
	}
}

// save stores every generated image under a fresh name. A store failure
// fails the call: an image the user cannot reload is not a result.
func (t *GenerateImageTool) save(ctx context.Context, images []imagegen.Image) ([]savedImage, error) {
	if t.store == nil {
		return nil, fmt.Errorf("image storage is unavailable")
	}
	out := make([]savedImage, 0, len(images))
	for i, img := range images {
		if len(img.Data) == 0 {
			return nil, fmt.Errorf("image %d is empty", i+1)
		}
		mime := detectImageMIME(img)
		if mime == "" {
			return nil, fmt.Errorf("image %d is not a recognised image format", i+1)
		}
		name := uuid.NewString() + imageExtension(mime)
		ref, err := t.store.Save(ctx, img.Data, name)
		if err != nil {
			return nil, err
		}
		saved := savedImage{
			Ref: ref, FileName: name, MIMEType: mime,
			RevisedPrompt: strings.TrimSpace(img.RevisedPrompt), Size: int64(len(img.Data)),
		}
		if config, _, err := image.DecodeConfig(bytes.NewReader(img.Data)); err == nil {
			saved.Width, saved.Height = config.Width, config.Height
		}
		out = append(out, saved)
	}
	return out, nil
}

// parseAttachmentImageRef accepts attachment://att_x (its first image) and
// attachment://att_x/images/N, the forms read_file advertises.
func parseAttachmentImageRef(ref string) (string, int, error) {
	rest := strings.Trim(strings.TrimPrefix(ref, types.AttachmentPathScheme), "/")
	handle, sub, _ := strings.Cut(rest, "/")
	if handle == "" {
		return "", 0, fmt.Errorf("name one attachment, for example attachment://att_xxxx")
	}
	if sub == "" {
		return handle, 1, nil
	}
	n, err := strconv.Atoi(strings.TrimPrefix(sub, attachmentImagesPath+"/"))
	if !strings.HasPrefix(sub, attachmentImagesPath+"/") || err != nil || n < 1 {
		return "", 0, fmt.Errorf("use attachment://%s or attachment://%s/images/N with N from 1", handle, handle)
	}
	return handle, n, nil
}

// normalizeImageSize accepts WIDTHxHEIGHT (also with X, × or *); empty keeps
// the model's default.
func normalizeImageSize(size string) (string, bool) {
	size = strings.TrimSpace(size)
	if size == "" {
		return "", true
	}
	m := imageSizePattern.FindStringSubmatch(size)
	if m == nil {
		return "", false
	}
	return m[1] + "x" + m[2], true
}

// detectImageMIME trusts the bytes over the vendor's label.
func detectImageMIME(img imagegen.Image) string {
	if sniffed := http.DetectContentType(img.Data); strings.HasPrefix(sniffed, "image/") {
		return sniffed
	}
	if mime := strings.ToLower(strings.TrimSpace(img.MIMEType)); strings.HasPrefix(mime, "image/") {
		return mime
	}
	return ""
}

func imageExtension(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".png"
	}
}
