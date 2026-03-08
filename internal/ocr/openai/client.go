package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string        `json:"role"`
	Content []contentItem `json:"content"`
}

type contentItem struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	ImageURL map[string]any `json:"image_url,omitempty"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c Client) Name() string {
	return "openai-compatible-vision"
}

func (c Client) Ready() bool {
	return strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.Model) != ""
}

func (c Client) RecognizeImage(ctx context.Context, imagePath string) (string, float64, error) {
	if !c.Ready() {
		return "", 0, errors.New("OCR 尚未配置，请先填写 OCR_API_KEY 与 OCR_MODEL")
	}
	raw, err := os.ReadFile(imagePath)
	if err != nil {
		return "", 0, err
	}
	mimeType := "image/png"
	if ext := strings.ToLower(filepath.Ext(imagePath)); ext == ".jpg" || ext == ".jpeg" {
		mimeType = "image/jpeg"
	}
	imageURL := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
	requestBody := chatCompletionRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{
				Role: "user",
				Content: []contentItem{
					{Type: "text", Text: "你是视频字幕 OCR。请只返回图片中的字幕文字；如果没有清晰可读字幕，返回空字符串。不要解释，不要添加标点说明，不要输出代码块。保留必要换行。"},
					{Type: "image_url", ImageURL: map[string]any{"url": imageURL}},
				},
			},
		},
		Temperature: 0,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", 0, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	request.Header.Set("Content-Type", "application/json")
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", 0, err
	}
	var payload chatCompletionResponse
	if err := json.Unmarshal(responseBody, &payload); err == nil && payload.Error != nil && payload.Error.Message != "" {
		return "", 0, errors.New(payload.Error.Message)
	}
	if response.StatusCode >= 400 {
		return "", 0, fmt.Errorf("OCR 请求失败: %s", strings.TrimSpace(string(responseBody)))
	}
	if len(payload.Choices) == 0 {
		return "", 0, errors.New("OCR 未返回可用结果")
	}
	text := strings.TrimSpace(payload.Choices[0].Message.Content)
	return text, 1, nil
}
