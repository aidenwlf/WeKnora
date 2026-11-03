package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tencent/WeKnora/internal/im"
	ws "github.com/gorilla/websocket"
)

func TestLongConnSendFileUploadsChunksAndSendsMedia(t *testing.T) {
	var (
		mu       sync.Mutex
		chunks   []int
		sendBody map[string]interface{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&ws.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame wsFrame
			if err := json.Unmarshal(msg, &frame); err != nil {
				return
			}
			reqID := frame.Headers["req_id"]
			var body interface{}
			switch frame.Cmd {
			case "aibot_upload_media_init":
				body = map[string]interface{}{"upload_id": "upload-1"}
			case "aibot_upload_media_chunk":
				var chunk struct {
					ChunkIndex int    `json:"chunk_index"`
					Base64Data string `json:"base64_data"`
				}
				_ = json.Unmarshal(frame.Body, &chunk)
				mu.Lock()
				chunks = append(chunks, chunk.ChunkIndex)
				mu.Unlock()
				body = map[string]interface{}{}
			case "aibot_upload_media_finish":
				body = map[string]interface{}{"media_id": "media-1"}
			case "aibot_send_msg":
				_ = json.Unmarshal(frame.Body, &sendBody)
				body = map[string]interface{}{}
			default:
				body = map[string]interface{}{}
			}
			bodyBytes, _ := json.Marshal(body)
			resp, _ := json.Marshal(wsFrame{
				Headers: map[string]string{"req_id": reqID},
				ErrCode: 0,
				ErrMsg:  "ok",
				Body:    bodyBytes,
			})
			if err := conn.WriteMessage(ws.TextMessage, resp); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := ws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	client := &LongConnClient{conn: conn, pending: make(map[string]chan wsFrame)}
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame wsFrame
			if json.Unmarshal(msg, &frame) == nil {
				client.resolvePending(frame)
			}
		}
	}()

	data := make([]byte, wecomUploadChunkSize+16)
	err = client.SendFile(context.Background(),
		&im.IncomingMessage{UserID: "user-1", ChatType: im.ChatTypeDirect},
		&im.OutgoingFile{FileName: "report.docx", Data: data},
	)
	if err != nil {
		t.Fatalf("SendFile: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(chunks) != 2 || chunks[0] != 0 || chunks[1] != 1 {
		t.Fatalf("chunk indexes = %v, want [0 1]", chunks)
	}
	if sendBody["chatid"] != "user-1" || sendBody["msgtype"] != "file" {
		t.Fatalf("send body = %#v", sendBody)
	}
	file, _ := sendBody["file"].(map[string]interface{})
	if file["media_id"] != "media-1" {
		t.Fatalf("media_id = %#v", file["media_id"])
	}
}
