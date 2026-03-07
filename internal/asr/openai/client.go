package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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

type verboseResponse struct {
	Text     string            `json:"text"`
	Segments []segmentResponse `json:"segments"`
}

type segmentResponse struct {
	ID    int     `json:"id"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

func (c Client) Name() string {
	return "openai-compatible"
}

func (c Client) Ready() bool {
	return strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.Model) != "" && strings.TrimSpace(c.BaseURL) != ""
}

func (c Client) Transcribe(ctx context.Context, audioPath string, sourceLanguage string) ([]subtitle.Block, error) {
	if !c.Ready() {
		return nil, errors.New("ASR 尚未配置，请先填写 ASR_API_KEY")
	}
	contentType, body, err := c.buildMultipartBody(audioPath, sourceLanguage)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/audio/transcriptions"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	request.Header.Set("Content-Type", contentType)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Minute}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var payload verboseResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("ASR 响应解析失败: %w", err)
	}
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("ASR 请求失败: %s", payload.Text)
	}
	if len(payload.Segments) == 0 {
		return nil, errors.New("ASR 未返回带时间轴的 segment，请确认模型支持 verbose_json + segment 时间戳")
	}
	blocks := make([]subtitle.Block, 0, len(payload.Segments))
	for index, segment := range payload.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		blocks = append(blocks, subtitle.Block{
			Index: index + 1,
			Start: time.Duration(segment.Start * float64(time.Second)),
			End:   time.Duration(segment.End * float64(time.Second)),
			Lines: []string{text},
		})
	}
	if len(blocks) == 0 {
		return nil, errors.New("ASR segment 为空")
	}
	return blocks, nil
}

func (c Client) buildMultipartBody(audioPath string, sourceLanguage string) (string, *bytes.Buffer, error) {
	file, err := os.Open(audioPath)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = file.Close() }()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fileWriter, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", nil, err
	}
	if _, err := file.WriteTo(fileWriter); err != nil {
		return "", nil, err
	}
	_ = writer.WriteField("model", c.Model)
	_ = writer.WriteField("response_format", "verbose_json")
	_ = writer.WriteField("timestamp_granularities[]", "segment")
	if strings.TrimSpace(sourceLanguage) != "" && strings.TrimSpace(sourceLanguage) != "auto" {
		_ = writer.WriteField("language", normalizeLanguageCode(sourceLanguage))
	}
	if err := writer.Close(); err != nil {
		return "", nil, err
	}
	return writer.FormDataContentType(), body, nil
}

func normalizeLanguageCode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "zh-cn" || value == "zh_cn" {
		return "zh"
	}
	if value == "en-us" || value == "en_us" {
		return "en"
	}
	if len(value) > 2 {
		parts := strings.FieldsFunc(value, func(char rune) bool {
			return char == '-' || char == '_'
		})
		if len(parts) > 0 {
			return parts[0]
		}
	}
	return value
}

func parseFloatToDuration(value string) time.Duration {
	seconds, _ := strconv.ParseFloat(value, 64)
	return time.Duration(seconds * float64(time.Second))
}
