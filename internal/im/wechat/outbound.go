package wechat

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/im"
	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

const wechatMaxFileBytes = 20 << 20

// SendFile uploads the artifact to the WeChat CDN and sends it as a FILE item.
// iLink media uses AES-128-ECB/PKCS7 with a fresh per-file key; the encrypted
// query parameter returned by the CDN is what the recipient uses to fetch it.
func (a *Adapter) SendFile(ctx context.Context, incoming *im.IncomingMessage, file *im.OutgoingFile) error {
	if incoming == nil || file == nil {
		return fmt.Errorf("missing incoming message or file")
	}
	if len(file.Data) == 0 {
		return fmt.Errorf("cannot send an empty file")
	}
	if len(file.Data) > wechatMaxFileBytes {
		return fmt.Errorf("file exceeds the WeChat %d-byte limit", wechatMaxFileBytes)
	}
	contextToken := ""
	if incoming.Extra != nil {
		contextToken = strings.TrimSpace(incoming.Extra["context_token"])
	}
	if contextToken == "" {
		return fmt.Errorf("missing context_token for WeChat reply")
	}

	fileKeyBytes := make([]byte, 16)
	aesKeyBytes := make([]byte, 16)
	if _, err := rand.Read(fileKeyBytes); err != nil {
		return fmt.Errorf("generate file key: %w", err)
	}
	if _, err := rand.Read(aesKeyBytes); err != nil {
		return fmt.Errorf("generate aes key: %w", err)
	}
	fileKey := hex.EncodeToString(fileKeyBytes)
	aesKeyHex := hex.EncodeToString(aesKeyBytes)
	plaintextSize := len(file.Data)
	cipherSize := ((plaintextSize / aes.BlockSize) + 1) * aes.BlockSize
	sum := md5.Sum(file.Data)

	uploadResp, err := a.ilinkPostRaw(ctx, "/ilink/bot/getuploadurl", map[string]interface{}{
		"filekey":       fileKey,
		"media_type":    3, // FILE
		"to_user_id":    incoming.UserID,
		"rawsize":       plaintextSize,
		"rawfilemd5":    hex.EncodeToString(sum[:]),
		"filesize":      cipherSize,
		"no_need_thumb": true,
		"aeskey":        aesKeyHex,
		"base_info":     newBaseInfo(),
	})
	if err != nil {
		return fmt.Errorf("get WeChat upload URL: %w", err)
	}
	var upload struct {
		UploadParam   string `json:"upload_param"`
		UploadFullURL string `json:"upload_full_url"`
	}
	if err := json.Unmarshal(uploadResp, &upload); err != nil {
		return fmt.Errorf("decode WeChat upload URL: %w", err)
	}

	cdnURL := strings.TrimSpace(upload.UploadFullURL)
	if cdnURL == "" {
		if upload.UploadParam == "" {
			return fmt.Errorf("WeChat upload URL missing")
		}
		query := url.Values{}
		query.Set("encrypted_query_param", upload.UploadParam)
		query.Set("filekey", fileKey)
		cdnURL = cdnBaseURL + "/upload?" + query.Encode()
	}
	if err := secutils.ValidateURLForSSRF(cdnURL); err != nil {
		return fmt.Errorf("WeChat CDN upload URL rejected: %w", err)
	}
	ciphertext, err := encryptWeChatMedia(file.Data, aesKeyBytes)
	if err != nil {
		return fmt.Errorf("encrypt WeChat media: %w", err)
	}
	downloadParam, err := uploadWeChatCDN(ctx, cdnURL, ciphertext)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"msg": map[string]interface{}{
			"from_user_id":  "",
			"to_user_id":    incoming.UserID,
			"client_id":     fmt.Sprintf("weknora_%d", time.Now().UnixNano()),
			"message_type":  2,
			"message_state": 2,
			"context_token": contextToken,
			"item_list": []map[string]interface{}{{
				"type": 4, // FILE
				"file_item": map[string]interface{}{
					"media": map[string]interface{}{
						"encrypt_query_param": downloadParam,
						// File/voice/video keys are base64 of the hex string.
						"aes_key":      base64.StdEncoding.EncodeToString([]byte(aesKeyHex)),
						"encrypt_type": 1,
					},
					"file_name": file.FileName,
					"len":       strconv.Itoa(plaintextSize),
				},
			}},
		},
		"base_info": newBaseInfo(),
	}
	if err := a.ilinkPost(ctx, "/ilink/bot/sendmessage", payload); err != nil {
		return fmt.Errorf("send WeChat file message: %w", err)
	}
	logger.Infof(ctx, "[WeChat] File message sent: user=%s name=%s size=%d", incoming.UserID, file.FileName, plaintextSize)
	return nil
}

func encryptWeChatMedia(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := block.BlockSize() - len(plaintext)%block.BlockSize()
	buf := make([]byte, len(plaintext)+padding)
	copy(buf, plaintext)
	for i := len(plaintext); i < len(buf); i++ {
		buf[i] = byte(padding)
	}
	for i := 0; i < len(buf); i += block.BlockSize() {
		block.Encrypt(buf[i:i+block.BlockSize()], buf[i:i+block.BlockSize()])
	}
	return buf, nil
}

func uploadWeChatCDN(ctx context.Context, endpoint string, ciphertext []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ciphertext))
	if err != nil {
		return "", fmt.Errorf("create WeChat CDN request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := ilinkHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload WeChat CDN: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("WeChat CDN upload returned %d: %s", resp.StatusCode, string(body))
	}
	downloadParam := strings.TrimSpace(resp.Header.Get("x-encrypted-param"))
	if downloadParam == "" {
		return "", fmt.Errorf("WeChat CDN upload returned no x-encrypted-param")
	}
	return downloadParam, nil
}
