package imagegen

import "github.com/Tencent/WeKnora/internal/types"

// Config holds what New needs to build an ImageGenerator for one model row.
type Config struct {
	Source    types.ModelSource
	BaseURL   string
	ModelName string
	APIKey    string
	ModelID   string
	// Provider is the vendor id stored on the row; empty detects it from
	// BaseURL.
	Provider      string
	Spec          *types.ModelSpecOverride
	ExtraConfig   map[string]string
	CustomHeaders map[string]string
}

// ConfigFromModel maps a stored or test-form model row onto Config. The
// production path and the connection check share it.
func ConfigFromModel(m *types.Model) *Config {
	if m == nil {
		return nil
	}
	return &Config{
		ModelID:       m.ID,
		APIKey:        m.Parameters.APIKey,
		BaseURL:       m.Parameters.BaseURL,
		ModelName:     m.Name,
		Source:        m.Source,
		Provider:      m.Parameters.Provider,
		Spec:          m.Parameters.Spec,
		ExtraConfig:   m.Parameters.ExtraConfig,
		CustomHeaders: m.Parameters.CustomHeaders,
	}
}

// New resolves the model through the catalog and returns its protocol
// client.
func New(config *Config) (ImageGenerator, error) {
	g, err := newGenerator(config)
	return wrapLangfuse(g, err)
}
