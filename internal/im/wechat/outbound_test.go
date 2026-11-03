package wechat

import (
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/im"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

func TestSendFileUploadsEncryptedMediaAndSendsFileItem(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)

	var (
		uploadedCipher []byte
		sent           map[string]interface{}
		mediaType      float64
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getuploadurl":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mediaType, _ = body["media_type"].(float64)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"upload_full_url": serverURL(r) + "/upload",
			})
		case "/upload":
			uploadedCipher, _ = io.ReadAll(r.Body)
			w.Header().Set("x-encrypted-param", "download-param")
			w.WriteHeader(http.StatusOK)
		case "/ilink/bot/sendmessage":
			_ = json.NewDecoder(r.Body).Decode(&sent)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ret": 0})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldBase, oldCDN := ilinkBaseURL, cdnBaseURL
	ilinkBaseURL, cdnBaseURL = server.URL, server.URL
	t.Cleanup(func() { ilinkBaseURL, cdnBaseURL = oldBase, oldCDN })

	adapter := NewAdapter("token", "bot")
	err := adapter.SendFile(context.Background(),
		&im.IncomingMessage{UserID: "user-1", Extra: map[string]string{"context_token": "ctx"}},
		&im.OutgoingFile{FileName: "report.docx", Data: []byte("docx bytes")},
	)
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}

	msg := sent["msg"].(map[string]interface{})
	items := msg["item_list"].([]interface{})
	item := items[0].(map[string]interface{})
	if item["type"].(float64) != 4 {
		t.Fatalf("item type = %v, want 4", item["type"])
	}
	file := item["file_item"].(map[string]interface{})
	media := file["media"].(map[string]interface{})
	if media["encrypt_query_param"] != "download-param" {
		t.Fatalf("encrypt_query_param = %v", media["encrypt_query_param"])
	}
	if file["file_name"] != "report.docx" || file["len"] != "10" {
		t.Fatalf("file_item = %#v", file)
	}

	// Decrypt the uploaded ciphertext with the key carried in the message.
	aesKeyB64 := media["aes_key"].(string)
	decoded, err := base64.StdEncoding.DecodeString(aesKeyB64)
	if err != nil {
		t.Fatalf("decode aes_key: %v", err)
	}
	key, err := hex.DecodeString(string(decoded))
	if err != nil || len(key) != 16 {
		t.Fatalf("aes key = %q err=%v", decoded, err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	plain := make([]byte, len(uploadedCipher))
	for i := 0; i < len(uploadedCipher); i += aes.BlockSize {
		block.Decrypt(plain[i:i+aes.BlockSize], uploadedCipher[i:i+aes.BlockSize])
	}
	pad := int(plain[len(plain)-1])
	if string(plain[:len(plain)-pad]) != "docx bytes" {
		t.Fatalf("decrypted = %q", plain[:len(plain)-pad])
	}

	uploadedCipher = nil
	if err := adapter.SendFile(context.Background(),
		&im.IncomingMessage{UserID: "user-1", Extra: map[string]string{"context_token": "ctx"}},
		&im.OutgoingFile{FileName: "chart.png", Data: []byte("png"), Image: true},
	); err != nil {
		t.Fatalf("SendFile image: %v", err)
	}
	if mediaType != 1 {
		t.Fatalf("image media_type = %v, want 1", mediaType)
	}
	msg = sent["msg"].(map[string]interface{})
	items = msg["item_list"].([]interface{})
	item = items[0].(map[string]interface{})
	if item["type"].(float64) != 2 {
		t.Fatalf("image item type = %v, want 2", item["type"])
	}
	if _, ok := item["image_item"]; !ok {
		t.Fatalf("image item = %#v", item)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}
