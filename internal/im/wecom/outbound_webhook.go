package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/Tencent/WeKnora/internal/im"
	"github.com/Tencent/WeKnora/internal/logger"
)

// Compile-time check that WebhookAdapter can deliver outbound files.
var _ im.FileSender = (*WebhookAdapter)(nil)

// SendFile uploads a temporary media file and sends it through the self-built
// app APIs. Group chats use appchat/send when available; direct messages and
// appchat failures fall back to message/send with the configured agent id.
func (a *WebhookAdapter) SendFile(ctx context.Context, incoming *im.IncomingMessage, file *im.OutgoingFile) error {
	if incoming == nil || file == nil {
		return fmt.Errorf("missing incoming message or file")
	}
	if len(file.Data) == 0 {
		return fmt.Errorf("cannot send an empty file")
	}
	limit := wecomMaxFileBytes
	mediaType := "file"
	msgType := "file"
	if file.Image {
		limit = wecomMaxImageBytes
		mediaType = "image"
		msgType = "image"
	}
	if len(file.Data) > limit {
		return fmt.Errorf("file exceeds the WeCom %d-byte %s limit", limit, mediaType)
	}

	accessToken, err := a.getAccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}
	mediaID, err := a.uploadMedia(ctx, accessToken, mediaType, file)
	if err != nil {
		return err
	}

	payloadFile := map[string]string{"media_id": mediaID}
	if incoming.ChatType == im.ChatTypeGroup && strings.TrimSpace(incoming.ChatID) != "" {
		if err := a.sendFileToAppChat(ctx, accessToken, incoming.ChatID, msgType, payloadFile); err == nil {
			logger.Infof(ctx, "[WeCom] File message sent: chat=%s name=%s size=%d", incoming.ChatID, file.FileName, len(file.Data))
			return nil
		} else {
			logger.Debugf(ctx, "[WeCom] appchat file send failed for chat=%s, falling back to touser: %v", incoming.ChatID, err)
		}
	}
	if err := a.sendFileToUser(ctx, accessToken, incoming.UserID, msgType, payloadFile); err != nil {
		return err
	}
	logger.Infof(ctx, "[WeCom] File message sent: user=%s name=%s size=%d", incoming.UserID, file.FileName, len(file.Data))
	return nil
}

func (a *WebhookAdapter) uploadMedia(ctx context.Context, accessToken, mediaType string, file *im.OutgoingFile) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", file.FileName)
	if err != nil {
		return "", fmt.Errorf("create media form: %w", err)
	}
	if _, err := part.Write(file.Data); err != nil {
		return "", fmt.Errorf("write media form: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close media form: %w", err)
	}

	uploadURL := fmt.Sprintf("%s/cgi-bin/media/upload?access_token=%s&type=%s",
		a.apiBaseURL, url.QueryEscape(accessToken), url.QueryEscape(mediaType))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &body)
	if err != nil {
		return "", fmt.Errorf("create media upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload media: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MediaID string `json:"media_id"`
	}
	if err := im.DecodeIMResponse(resp.Body, &result); err != nil {
		return "", fmt.Errorf("decode media upload response: %w", err)
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("wecom media upload error: code=%d msg=%s", result.ErrCode, result.ErrMsg)
	}
	if strings.TrimSpace(result.MediaID) == "" {
		return "", fmt.Errorf("wecom media upload returned no media_id")
	}
	return result.MediaID, nil
}

func (a *WebhookAdapter) sendFileToAppChat(ctx context.Context, accessToken, chatID, msgType string, media map[string]string) error {
	payload := map[string]interface{}{
		"chatid":  chatID,
		"msgtype": msgType,
		msgType:   media,
	}
	return a.postJSON(ctx, fmt.Sprintf("%s/cgi-bin/appchat/send?access_token=%s", a.apiBaseURL, url.QueryEscape(accessToken)), payload)
}

func (a *WebhookAdapter) sendFileToUser(ctx context.Context, accessToken, userID, msgType string, media map[string]string) error {
	payload := map[string]interface{}{
		"touser":  userID,
		"msgtype": msgType,
		"agentid": a.corpAgentID,
		msgType:   media,
	}
	return a.postJSON(ctx, fmt.Sprintf("%s/cgi-bin/message/send?access_token=%s", a.apiBaseURL, url.QueryEscape(accessToken)), payload)
}

func (a *WebhookAdapter) postJSON(ctx context.Context, endpoint string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send file message: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := im.DecodeIMResponse(resp.Body, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if result.ErrCode != 0 {
		return fmt.Errorf("wecom api error: code=%d msg=%s", result.ErrCode, result.ErrMsg)
	}
	return nil
}
