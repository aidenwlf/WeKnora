package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/stretchr/testify/require"
)

// off is the composer's 生图 switch, on is its opposite, nil is a caller that
// never sent one (IM, automation, API).
func TestApplyImageGenerationToggleNarrowsOnlyThisRun(t *testing.T) {
	off, on := false, true
	allowed := []string{tools.ToolWebSearch, tools.ToolGenerateImage}

	for _, tc := range []struct {
		name    string
		allowed []string
		enabled *bool
		want    []string
	}{
		{"unset keeps the agent configuration", allowed, nil, allowed},
		{"on keeps the agent configuration", allowed, &on, allowed},
		{"off removes the tool", allowed, &off, []string{tools.ToolWebSearch}},
		{"off without the tool changes nothing", []string{tools.ToolWebSearch}, &off, []string{tools.ToolWebSearch}},
		{"off on a nil list stays nil", nil, &off, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, applyImageGenerationToggle(tc.allowed, tc.enabled))
		})
	}

	// The caller's slice is the agent's configuration; the toggle must not
	// rewrite what the next run would see.
	require.Equal(t, []string{tools.ToolWebSearch, tools.ToolGenerateImage}, allowed)
}
