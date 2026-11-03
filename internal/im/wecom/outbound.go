package wecom

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/im"
	"github.com/Tencent/WeKnora/internal/logger"
)

const (
	wecomUploadChunkSize = 512 * 1024
	wecomMaxUploadChunks = 100
	wecomMaxFileBytes    = 20 << 20
	wecomMaxImageBytes   = 10 << 20
	wecomRequestTimeout  = 30 * time.Second
)

// resolvePending delivers a response frame to the request waiting for its
// req_id. It reports true when the frame was consumed.
func (c *LongConnClient) resolvePending(frame wsFrame) bool {
	if frame.Headers == nil {
		return false
	}
	reqID := frame.Headers["req_id"]
	if reqID == "" {
		return false
	}
	c.pendingMu.Lock()
	ch, ok := c.pending[reqID]
	c.pendingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- frame:
	default:
	}
	return true
}

// request sends one command frame and waits for the reply carrying the same
// req_id. Upload and active-send commands use this path; callbacks keep using
// the existing handler dispatch.
func (c *LongConnClient) request(ctx context.Context, cmd string, body interface{}) (wsFrame, error) {
	reqID := fmt.Sprintf("%s_%d", cmd, c.reqSeq.Add(1))
	ch := make(chan wsFrame, 1)

	c.pendingMu.Lock()
	if c.pending == nil {
		c.pending = make(map[string]chan wsFrame)
	}
	c.pending[reqID] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
	}()

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return wsFrame{}, fmt.Errorf("marshal %s body: %w", cmd, err)
	}
	if err := c.writeJSON(wsFrame{
		Cmd:     cmd,
		Headers: map[string]string{"req_id": reqID},
		Body:    bodyBytes,
	}); err != nil {
		return wsFrame{}, fmt.Errorf("send %s: %w", cmd, err)
	}

	timer := time.NewTimer(wecomRequestTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return wsFrame{}, ctx.Err()
	case <-timer.C:
		return wsFrame{}, fmt.Errorf("%s timed out", cmd)
	case resp := <-ch:
		if resp.ErrCode != 0 {
			return wsFrame{}, fmt.Errorf("%s failed: code=%d msg=%s", cmd, resp.ErrCode, resp.ErrMsg)
		}
		return resp, nil
	}
}

// uploadMedia runs the aibot three-step temporary-media upload (init, chunks,
// finish) and returns the media_id used by a media reply.
func (c *LongConnClient) uploadMedia(ctx context.Context, mediaType, filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("cannot upload an empty file")
	}
	if len(data) > wecomMaxFileBytes {
		return "", fmt.Errorf("file exceeds the WeCom %d-byte limit", wecomMaxFileBytes)
	}
	totalChunks := (len(data) + wecomUploadChunkSize - 1) / wecomUploadChunkSize
	if totalChunks > wecomMaxUploadChunks {
		return "", fmt.Errorf("file needs %d chunks, maximum is %d", totalChunks, wecomMaxUploadChunks)
	}

	initResp, err := c.request(ctx, "aibot_upload_media_init", map[string]interface{}{
		"type":         mediaType,
		"filename":     filename,
		"total_size":   len(data),
		"total_chunks": totalChunks,
		"md5":          fmt.Sprintf("%x", md5.Sum(data)),
	})
	if err != nil {
		return "", err
	}
	var initBody struct {
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(initResp.Body, &initBody); err != nil {
		return "", fmt.Errorf("decode upload init: %w", err)
	}
	if strings.TrimSpace(initBody.UploadID) == "" {
		return "", fmt.Errorf("upload init returned no upload_id")
	}

	// The official SDK sends zero-based chunk indexes.
	for index, start := 0, 0; start < len(data); index, start = index+1, start+wecomUploadChunkSize {
		end := start + wecomUploadChunkSize
		if end > len(data) {
			end = len(data)
		}
		if _, err := c.request(ctx, "aibot_upload_media_chunk", map[string]interface{}{
			"upload_id":   initBody.UploadID,
			"chunk_index": index,
			"base64_data": base64.StdEncoding.EncodeToString(data[start:end]),
		}); err != nil {
			return "", fmt.Errorf("upload chunk %d/%d: %w", index+1, totalChunks, err)
		}
	}

	finishResp, err := c.request(ctx, "aibot_upload_media_finish", map[string]interface{}{
		"upload_id": initBody.UploadID,
	})
	if err != nil {
		return "", err
	}
	var finishBody struct {
		MediaID string `json:"media_id"`
	}
	if err := json.Unmarshal(finishResp.Body, &finishBody); err != nil {
		return "", fmt.Errorf("decode upload finish: %w", err)
	}
	if strings.TrimSpace(finishBody.MediaID) == "" {
		return "", fmt.Errorf("upload finish returned no media_id")
	}
	return finishBody.MediaID, nil
}

// SendFile uploads the file and pushes it through aibot_send_msg. Active send
// is used instead of a second passive reply: after a streaming answer ended,
// the original req_id no longer owns a reply slot.
func (c *LongConnClient) SendFile(ctx context.Context, incoming *im.IncomingMessage, file *im.OutgoingFile) error {
	if incoming == nil || file == nil {
		return fmt.Errorf("missing incoming message or file")
	}
	chatID := strings.TrimSpace(incoming.ChatID)
	if chatID == "" {
		// WeCom active messages address a direct chat by the user's id and a
		// group by its chatid.
		chatID = strings.TrimSpace(incoming.UserID)
	}
	if chatID == "" {
		return fmt.Errorf("cannot resolve WeCom chat id")
	}

	mediaType := "file"
	msgType := "file"
	payloadKey := "file"
	limit := wecomMaxFileBytes
	if file.Image {
		mediaType = "image"
		msgType = "image"
		payloadKey = "image"
		limit = wecomMaxImageBytes
	}
	if len(file.Data) > limit {
		return fmt.Errorf("file exceeds the WeCom %d-byte %s limit", limit, mediaType)
	}

	mediaID, err := c.uploadMedia(ctx, mediaType, file.FileName, file.Data)
	if err != nil {
		return fmt.Errorf("upload WeCom media: %w", err)
	}

	if _, err := c.request(ctx, "aibot_send_msg", map[string]interface{}{
		"chatid":   chatID,
		"msgtype":  msgType,
		payloadKey: map[string]string{"media_id": mediaID},
	}); err != nil {
		return fmt.Errorf("send WeCom %s message: %w", msgType, err)
	}
	logger.Infof(ctx, "[WeCom] %s message sent: chat=%s name=%s size=%d", msgType, chatID, file.FileName, len(file.Data))
	return nil
}
