package imagegen

import (
	"context"
	"net/url"
	"strings"
	"testing"

	modelruntime "github.com/Tencent/WeKnora/internal/models/runtime"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

// Every catalogued image model generates from text at its vendor's default
// path, and those that take reference images reach the edits path.
func TestEveryCatalogImageModelReachesTheWire(t *testing.T) {
	for _, v := range modelruntime.List() {
		for _, m := range v.Models() {
			if m.Type != types.ModelTypeImageGeneration {
				continue
			}
			name := m.ID
			if name == "" {
				name = strings.ReplaceAll(m.Match, "*", "snapshot")
			}
			t.Run(v.ID+"/"+name, func(t *testing.T) {
				address, got, calls := upstream(t)
				// Azure's default is a {resource} template, which has no path.
				path := ""
				if base, err := url.Parse(v.GetDefaultURL(types.ModelTypeImageGeneration)); err == nil {
					path = base.Path
				}
				g, err := newGenerator(&Config{
					Provider: v.ID, ModelName: name, ModelID: "row-id",
					BaseURL: address + path, APIKey: "row-key",
				})
				if strings.Contains(string(m.Compat), "unsupported_reason") {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				out, err := g.Generate(context.Background(), Request{Prompt: "a red circle", Size: CheckSize(g)})
				require.NoError(t, err)
				require.Len(t, out.Images, 1)
				require.Equal(t, "image/png", out.Images[0].MIMEType)
				require.True(t, strings.HasSuffix(got.path, "/images/generations"), got.path)
				require.Equal(t, name, got.body["model"])
				if g.MaxInputImages() == 0 {
					return
				}
				_, err = g.Generate(context.Background(), Request{Prompt: "edit", Images: []InputImage{reference()}})
				require.NoError(t, err)
				require.True(t, strings.HasSuffix(got.path, "/images/edits"), got.path)
				require.EqualValues(t, 2, calls.Load())
			})
		}
	}
}
