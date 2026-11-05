package runtime

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Tencent/WeKnora/internal/models"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/internal/configcopy"
)

// resolveImages merges the image generation layers, in the same order as
// resolveTranscriptions: protocol default, vendor compat, the catalog entry,
// the row's own compat, then remote_model_name.
func resolveImages(
	ref Ref, vendor *Provider, spec models.ModelSpec, cataloged bool, baseURL string,
) (*Resolved, error) {
	protocol := vendor.ImageGenerationAPI
	if protocol == "" {
		protocol = api.ImageGenerationOpenAI
	}
	settings := api.DefaultImages()
	ApplyCompat(&settings, &vendor.Compat.Images)
	for _, raw := range []json.RawMessage{spec.Compat, ref.Override.CompatJSON()} {
		if len(raw) == 0 {
			continue
		}
		overlay := &api.ImagesCompat{}
		if err := DecodeCompat(raw, overlay); err != nil {
			return nil, fmt.Errorf("images compat: %w", err)
		}
		ApplyCompat(&settings, overlay)
	}
	if settings.API != "" {
		protocol = settings.API
	}
	if !protocol.Known() {
		return nil, fmt.Errorf("catalog: unknown image generation api %q for %s/%s", protocol, vendor.ID, spec.ID)
	}
	settings.API = protocol
	if err := checkImagesSettings(settings); err != nil {
		return nil, fmt.Errorf("catalog: %s/%s: %w", vendor.ID, spec.ID, err)
	}
	// Checked after every layer, like the transcription refusal.
	if settings.UnsupportedReason != "" {
		return nil, &UnsupportedModelError{Provider: vendor.ID, Model: spec.ID, Reason: settings.UnsupportedReason}
	}

	out := &Resolved{
		Vendor:             configcopy.Clone(vendor.Definition),
		Spec:               spec,
		Cataloged:          cataloged,
		BaseURL:            baseURL,
		RemoteModel:        ref.Model,
		ImageGenerationAPI: protocol,
		Images:             settings,
	}
	if override := strings.TrimSpace(ref.Extra[models.ExtraRemoteModelName]); override != "" {
		out.RemoteModel = override
	}
	return out, nil
}

func checkImagesSettings(s api.ImagesSettings) error {
	if s.MaxInputImages < 0 {
		return fmt.Errorf("max_input_images %d is negative", s.MaxInputImages)
	}
	if s.MaxCount < 1 || s.MaxCount > api.MaxImageCount {
		return fmt.Errorf("max_count %d is outside 1..%d", s.MaxCount, api.MaxImageCount)
	}
	if s.DefaultSize != "" && len(s.Sizes) > 0 && !slices.Contains(s.Sizes, s.DefaultSize) {
		return fmt.Errorf("default_size %q is not one of sizes %v", s.DefaultSize, s.Sizes)
	}
	return nil
}

// imageGenerationInput reports what Generate accepts, whatever the catalog
// entry's input list says: reference images are allowed exactly when the
// resolved limit is above zero.
func imageGenerationInput(maxInputImages int) []string {
	if maxInputImages > 0 {
		return []string{"text", "image"}
	}
	return []string{"text"}
}
