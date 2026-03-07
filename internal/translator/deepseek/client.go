package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/subtitle"
)

type Client struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
}

type translationItem struct {
	Index       int    `json:"index"`
	SourceText  string `json:"source_text"`
	Translation string `json:"translation,omitempty"`
}

type translateResult struct {
	Items []translationItem `json:"items"`
}

type chatCompletionRequest struct {
	Model          string               `json:"model"`
	Messages       []chatMessage        `json:"messages"`
	Temperature    float64              `json:"temperature,omitempty"`
	ResponseFormat *responseFormatField `json:"response_format,omitempty"`
}

type responseFormatField struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c Client) Name() string {
	return "deepseek"
}

func (c Client) Ready() bool {
	return strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.Model) != ""
}

func (c Client) TranslateBlocks(ctx context.Context, prompt string, sourceLanguage string, targetLanguage string, blocks []subtitle.Block, batchSize int) ([]string, error) {
	if !c.Ready() {
		return nil, errors.New("DeepSeek 尚未配置，请先填写 DEEPSEEK_API_KEY")
	}
	if batchSize <= 0 {
		batchSize = 20
	}
	translations := make([]string, len(blocks))
	for offset := 0; offset < len(blocks); offset += batchSize {
		end := offset + batchSize
		if end > len(blocks) {
			end = len(blocks)
		}
		batch, err := c.translateBatch(ctx, prompt, sourceLanguage, targetLanguage, blocks[offset:end])
		if err != nil {
			return nil, err
		}
		copy(translations[offset:end], batch)
	}
	return translations, nil
}

func (c Client) translateBatch(ctx context.Context, prompt string, sourceLanguage string, targetLanguage string, blocks []subtitle.Block) ([]string, error) {
	items := make([]translationItem, 0, len(blocks))
	for _, block := range blocks {
		items = append(items, translationItem{Index: block.Index, SourceText: subtitle.JoinText(block.Lines)})
	}
	payloadJSON, err := json.Marshal(map[string]any{
		"source_language": sourceLanguage,
		"target_language": targetLanguage,
		"items":           items,
	})
	if err != nil {
		return nil, err
	}
	systemPrompt := strings.TrimSpace(prompt)
	if systemPrompt == "" {
		systemPrompt = "请逐条翻译字幕文本，只输出目标语言译文，不要解释，不要合并或拆分字幕。"
	}
	systemPrompt = systemPrompt + "\n返回严格 JSON，格式为 {\"items\":[{\"index\":1,\"translation\":\"...\"}]}。禁止输出额外说明。"
	requestBody := chatCompletionRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: string(payloadJSON)},
		},
		Temperature:    0.2,
		ResponseFormat: &responseFormatField{Type: "json_object"},
	}
	rawResponse, err := c.createChatCompletion(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	var result translateResult
	if err := json.Unmarshal([]byte(rawResponse), &result); err != nil {
		return nil, fmt.Errorf("DeepSeek 返回内容不是有效 JSON: %w", err)
	}
	if len(result.Items) != len(items) {
		return nil, fmt.Errorf("DeepSeek 返回条目数不匹配: 期望 %d，实际 %d", len(items), len(result.Items))
	}
	translations := make([]string, 0, len(items))
	resultMap := make(map[int]string, len(result.Items))
	for _, item := range result.Items {
		resultMap[item.Index] = strings.TrimSpace(item.Translation)
	}
	for _, item := range items {
		translation := resultMap[item.Index]
		if translation == "" {
			return nil, fmt.Errorf("第 %d 条字幕翻译为空", item.Index)
		}
		translations = append(translations, translation)
	}
	return translations, nil
}

func (c Client) createChatCompletion(ctx context.Context, payload chatCompletionRequest) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/chat/completions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	request.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return "", err
	}
	var completion chatCompletionResponse
	if err := json.Unmarshal(responseBody, &completion); err == nil && completion.Error != nil && completion.Error.Message != "" {
		return "", errors.New(completion.Error.Message)
	}
	if response.StatusCode >= 400 {
		return "", fmt.Errorf("DeepSeek 请求失败: %s", strings.TrimSpace(string(responseBody)))
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("DeepSeek 未返回可用结果")
	}
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("DeepSeek 返回空内容")
	}
	return content, nil
}
