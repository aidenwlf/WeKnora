package wecom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/im"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestWebhookSendFileUploadsAndSendsFileMessage(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	var uploaded []byte
	var sent map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0, "access_token": "token", "expires_in": 7200,
			})
		case "/cgi-bin/media/upload":
			if got := r.URL.Query().Get("type"); got != "file" {
				t.Errorf("media type = %q, want file", got)
			}
			file, _, err := r.FormFile("media")
			if err != nil {
				t.Errorf("read media: %v", err)
				http.Error(w, "bad media", http.StatusBadRequest)
				return
			}
			defer file.Close()
			uploaded, _ = io.ReadAll(file)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"errcode": 0, "media_id": "media-1",
			})
		case "/cgi-bin/message/send":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Errorf("decode send body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"errcode": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := &WebhookAdapter{
		corpID: "corp", agentSecret: "secret", corpAgentID: 7, apiBaseURL: server.URL,
	}
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
	if sent["msgtype"] != "file" {
		t.Fatalf("msgtype = %v, want file", sent["msgtype"])
	}
	file, _ := sent["file"].(map[string]interface{})
	if file["media_id"] != "media-1" {
		t.Fatalf("media_id = %v", file["media_id"])
	}
	if sent["touser"] != "user-1" {
		t.Fatalf("touser = %v", sent["touser"])
	}
}

func TestWebhookSendFileRejectsEmptyAndOversized(t *testing.T) {
	adapter := &WebhookAdapter{}
	if err := adapter.SendFile(context.Background(), &im.IncomingMessage{UserID: "u"}, &im.OutgoingFile{FileName: "x"}); err == nil ||
		!strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty file error = %v", err)
	}
	if err := adapter.SendFile(context.Background(), &im.IncomingMessage{UserID: "u"}, &im.OutgoingFile{
		FileName: "x", Data: make([]byte, wecomMaxFileBytes+1),
	}); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized file error = %v", err)
	}
}
