package api

// ImagesCompat is the overlay form (every field optional) of the image
// generation settings. JSON keys are the names used in models.json.
type ImagesCompat struct {
	API             *ImageGenerationAPI `json:"api,omitempty"`
	GenerationsPath *string             `json:"generations_path,omitempty"`
	EditsPath       *string             `json:"edits_path,omitempty"`
	// ResponseFormat is sent as response_format when non-empty. gpt-image
	// models reject the field and always answer b64_json; dall-e answers a
	// url unless asked, and some gateways need it named.
	ResponseFormat *string `json:"response_format,omitempty"`
	// MaxInputImages is how many reference images one edits request may
	// carry; 0 makes the model text-to-image only.
	MaxInputImages *int `json:"max_input_images,omitempty"`
	// MaxCount caps n where the model documents a lower limit than the
	// protocol's 10 (dall-e-3 takes only 1).
	MaxCount    *int    `json:"max_count,omitempty"`
	DefaultSize *string `json:"default_size,omitempty"`
	// Sizes is the closed list of sizes the model documents; empty means the
	// vendor decides.
	Sizes          []string `json:"sizes,omitempty"`
	RequestTimeout *int     `json:"request_timeout_seconds,omitempty"`
	// UnsupportedReason marks a model this build cannot call.
	UnsupportedReason *string `json:"unsupported_reason,omitempty"`
}

// ImagesSettings is the resolved (fully defaulted) form.
type ImagesSettings struct {
	API             ImageGenerationAPI
	GenerationsPath string
	EditsPath       string
	ResponseFormat  string
	MaxInputImages  int
	MaxCount        int
	// DefaultSize is sent when the request names no size; empty sends none
	// and the vendor uses its own default.
	DefaultSize string
	Sizes       []string
	// RequestTimeout caps one request, in seconds.
	RequestTimeout    int
	UnsupportedReason string
}

// MaxImageCount is the protocol's ceiling on n.
const MaxImageCount = 10

// DefaultImages is the protocol baseline: text-to-image only, the vendor's
// own size, and a deadline for a slow call. A high-quality gpt-image edit
// can take well over a minute.
func DefaultImages() ImagesSettings {
	return ImagesSettings{
		GenerationsPath: "/images/generations",
		EditsPath:       "/images/edits",
		MaxCount:        MaxImageCount,
		RequestTimeout:  300,
	}
}
