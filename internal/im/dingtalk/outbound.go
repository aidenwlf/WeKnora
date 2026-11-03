package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/im"
	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const dingtalkMaxFileBytes = 20 << 20

// dingtalkMediaBaseURL is the legacy OpenAPI host used by media/upload.
// The new api.dingtalk.com token cannot call this endpoint. It is a variable
// so tests can point it at a local server.
var dingtalkMediaBaseURL = "https://oapi.dingtalk.com"

// SendFile uploads the artifact and sends a native file message through the
// robot OpenAPI. Images use sampleImageMsg; every other artifact uses
// sampleFile, matching DingTalk's message templates.
func (a *Adapter) SendFile(ctx context.Context, incoming *im.IncomingMessage, file *im.OutgoingFile) error {
	if incoming == nil || file == nil {
		return fmt.Errorf("missing incoming message or file")
	}
	if len(file.Data) == 0 {
		return fmt.Errorf("cannot send an empty file")
	}
	if len(file.Data) > dingtalkMaxFileBytes {
		return fmt.Errorf("file exceeds the DingTalk %d-byte limit", dingtalkMaxFileBytes)
	}

	mediaType, msgKey, msgParam, err := a.prepareFileMessage(file)
	if err != nil {
		return err
	}
	mediaID, err := a.uploadMedia(ctx, mediaType, file.FileName, file.Data)
	if err != nil {
		return err
	}

	switch msgKey {
	case "sampleImageMsg":
		msgParam["photoURL"] = mediaID
	default:
		msgParam["mediaId"] = mediaID
	}

	msgParamJSON, err := json.Marshal(msgParam)
	if err != nil {
		return fmt.Errorf("marshal file message: %w", err)
	}

	// The stream callback carries a sessionWebhook, which is the cheapest path
	// and works without the app's robot OpenAPI send permission. Custom
	// webhooks do not accept file messages, but the session endpoint does.
	if webhook := strings.TrimSpace(incoming.Extra["session_webhook"]); webhook != "" {
		payload := map[string]interface{}{"msgtype": "file", "file": map[string]string{"media_id": mediaID}}
		if msgKey == "sampleImageMsg" {
			payload = map[string]interface{}{"msgtype": "image", "image": map[string]string{"media_id": mediaID}}
		}
		if err := a.postSessionWebhookPayload(ctx, webhook, payload); err == nil {
			logger.Infof(ctx, "[DingTalk] File message sent via sessionWebhook: user=%s name=%s size=%d",
				incoming.UserID, file.FileName, len(file.Data))
			return nil
		} else {
			logger.Debugf(ctx, "[DingTalk] sessionWebhook file send failed, falling back to OpenAPI: %v", err)
		}
	}

	body := map[string]interface{}{
		"robotCode": a.clientID,
		"msgKey":    msgKey,
		"msgParam":  string(msgParamJSON),
	}

	var path string
	if incoming.ChatType == im.ChatTypeGroup {
		path = "/v1.0/robot/groupMessages/send"
		body["openConversationId"] = incoming.ChatID
	} else {
		path = "/v1.0/robot/oToMessages/batchSend"
		body["userIds"] = []string{incoming.UserID}
	}
	if _, err := a.dingtalkAPI(ctx, http.MethodPost, path, body); err != nil {
		return fmt.Errorf("send DingTalk file message: %w", err)
	}
	logger.Infof(ctx, "[DingTalk] File message sent: user=%s name=%s size=%d", incoming.UserID, file.FileName, len(file.Data))
	return nil
}

func (a *Adapter) prepareFileMessage(file *im.OutgoingFile) (mediaType, msgKey string, msgParam map[string]interface{}, err error) {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(file.FileName)), ".")
	switch ext {
	case "jpg", "jpeg", "png", "gif", "bmp":
		return "image", "sampleImageMsg", map[string]interface{}{}, nil
	default:
		return "file", "sampleFile", map[string]interface{}{
			"fileName": file.FileName,
			"fileType": ext,
		}, nil
	}
}

func (a *Adapter) getLegacyAccessToken(ctx context.Context) (string, error) {
	a.legacyTokenMu.Lock()
	defer a.legacyTokenMu.Unlock()

	if a.legacyToken != "" && time.Now().Before(a.legacyTokenExpAt) {
		return a.legacyToken, nil
	}

	endpoint := fmt.Sprintf("%s/gettoken?appkey=%s&appsecret=%s",
		dingtalkMediaBaseURL, url.QueryEscape(a.clientID), url.QueryEscape(a.clientSecret))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create legacy token request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get legacy access token: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := im.DecodeIMResponse(resp.Body, &result); err != nil {
		return "", fmt.Errorf("decode legacy token response: %w", err)
	}
	if result.ErrCode != 0 {
		return "", fmt.Errorf("dingtalk legacy token error: code=%d msg=%s", result.ErrCode, result.ErrMsg)
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return "", fmt.Errorf("dingtalk legacy token is empty")
	}
	a.legacyToken = result.AccessToken
	expiresIn := result.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}
	ttl := time.Duration(expiresIn) * time.Second
	if ttl > 5*time.Minute {
		ttl -= 5 * time.Minute
	}
	a.legacyTokenExpAt = time.Now().Add(ttl)
	return a.legacyToken, nil
}

func (a *Adapter) uploadMedia(ctx context.Context, mediaType, filename string, data []byte) (string, error) {
	token, err := a.getLegacyAccessToken(ctx)
	if err != nil {
		return "", err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("media", filename)
	if err != nil {
		return "", fmt.Errorf("create media form: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write media form: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close media form: %w", err)
	}

	endpoint := fmt.Sprintf("%s/media/upload?access_token=%s&type=%s",
		dingtalkMediaBaseURL, url.QueryEscape(token), url.QueryEscape(mediaType))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
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
		return "", fmt.Errorf("dingtalk media upload error: code=%d msg=%s", result.ErrCode, result.ErrMsg)
	}
	if strings.TrimSpace(result.MediaID) == "" {
		return "", fmt.Errorf("dingtalk media upload returned no media_id")
	}
	return result.MediaID, nil
}

// postSessionWebhookPayload POSTs a robot payload to the per-message session
// webhook. The URL is validated with the same host/path/SSRF rules as text
// replies, so a forged callback cannot turn this into an open proxy.
func (a *Adapter) postSessionWebhookPayload(ctx context.Context, webhookURL string, payload interface{}) error {
	u, err := url.Parse(webhookURL)
	if err != nil || u.Scheme != "https" || u.Host != "oapi.dingtalk.com" ||
		u.User != nil || u.Path != "/robot/sendBySession" {
		return fmt.Errorf("untrusted dingtalk session webhook")
	}
	if err := secutils.ValidateURLForSSRF(webhookURL); err != nil {
		return fmt.Errorf("dingtalk sessionWebhook rejected by SSRF policy: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal session webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create session webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send session webhook file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := im.ReadIMResponse(resp.Body)
		return fmt.Errorf("dingtalk sessionWebhook returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
