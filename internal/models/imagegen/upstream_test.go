package imagegen

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

type wire struct {
	path, query, auth, apiKey string
	body                      map[string]any
	fields                    map[string]string
	files                     []string
}

// upstream records the request it receives and answers one b64_json image.
func upstream(t *testing.T) (string, *wire, *atomic.Int32) {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	got := &wire{}
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		*got = wire{fields: map[string]string{}}
		got.path, got.query = r.URL.Path, r.URL.RawQuery
		got.auth, got.apiKey = r.Header.Get("Authorization"), r.Header.Get("api-key")
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		if mediaType == "application/json" {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&got.body))
		} else {
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				data, _ := io.ReadAll(part)
				if part.FileName() != "" {
					got.files = append(got.files, part.FormName()+":"+part.Header.Get("Content-Type"))
					continue
				}
				got.fields[part.FormName()] = string(data)
			}
		}
		_, _ = w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(pngBytes) + `"}]}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, got, calls
}

func reference() InputImage {
	return InputImage{Data: pngBytes, MIMEType: "image/png", FileName: "ref.png"}
}
