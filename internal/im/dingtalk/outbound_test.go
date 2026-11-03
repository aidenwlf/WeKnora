package dingtalk

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/im"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestSendFileUploadsAndPostsSampleFile(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	var uploaded []byte
	var sent map[string]interface{}
	var mediaType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/oauth2/accessToken":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"accessToken": "new-token", "expireIn": 7200})
		case "/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"errcode": 0, "access_token": "legacy-token", "expires_in": 7200})
		case "/media/upload":
			mediaType = r.URL.Query().Get("type")
			if mediaType != "file" && mediaType != "image" {
				t.Errorf("media type = %q, want file or image", mediaType)
			}
			file, _, err := r.FormFile("media")
			if err != nil {
				t.Errorf("read media: %v", err)
				http.Error(w, "bad media", http.StatusBadRequest)
				return
			}
			defer file.Close()
			uploaded, _ = io.ReadAll(file)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"errcode": 0, "media_id": "media-1"})
		case "/v1.0/robot/oToMessages/batchSend":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Errorf("decode send body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldAPI, oldMedia := apiBaseURL, dingtalkMediaBaseURL
	apiBaseURL, dingtalkMediaBaseURL = server.URL, server.URL
	t.Cleanup(func() { apiBaseURL, dingtalkMediaBaseURL = oldAPI, oldMedia })

	adapter := &Adapter{clientID: "robot", clientSecret: "secret"}
	err := adapter.SendFile(context.Background(),
		&im.IncomingMessage{UserID: "user-1", ChatType: im.ChatTypeDirect},
		&im.OutgoingFile{FileName: "report.docx", Data: []byte("docx bytes")},
	)
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}
	if string(uploaded) != "docx bytes" {
		t.Fatalf("uploaded bytes = %q", uploaded)
	}
	if sent["msgKey"] != "sampleFile" {
		t.Fatalf("msgKey = %v, want sampleFile", sent["msgKey"])
	}
	param := sent["msgParam"].(string)
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(param), &params); err != nil {
		t.Fatalf("decode msgParam: %v", err)
	}
	if params["mediaId"] != "media-1" || params["fileName"] != "report.docx" || params["fileType"] != "docx" {
		t.Fatalf("msgParam = %#v", params)
	}
	users, _ := sent["userIds"].([]interface{})
	if len(users) != 1 || users[0] != "user-1" {
		t.Fatalf("userIds = %#v", sent["userIds"])
	}

	if err := adapter.SendFile(context.Background(),
		&im.IncomingMessage{UserID: "user-1", ChatType: im.ChatTypeDirect},
		&im.OutgoingFile{FileName: "chart.png", Data: []byte("png"), Image: true},
	); err != nil {
		t.Fatalf("SendFile image: %v", err)
	}
	if mediaType != "image" || sent["msgKey"] != "sampleImageMsg" {
		t.Fatalf("image media type = %q msgKey = %v", mediaType, sent["msgKey"])
	}
	var imageParams map[string]interface{}
	if err := json.Unmarshal([]byte(sent["msgParam"].(string)), &imageParams); err != nil {
		t.Fatalf("decode image msgParam: %v", err)
	}
	if imageParams["photoURL"] != "media-1" {
		t.Fatalf("image msgParam = %#v", imageParams)
	}
}
