package opensubtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/model"
	"github.com/gayhub/4subs/internal/provider"
)

const apiBase = "https://api.opensubtitles.com/api/v1"

type Client struct {
	httpClient       *http.Client
	languagePriority []string
}

func New(languagePriority []string) *Client {
	return &Client{
		httpClient:       &http.Client{Timeout: 20 * time.Second},
		languagePriority: languagePriority,
	}
}

func (c *Client) Name() string {
	return "opensubtitles"
}

type loginResponse struct {
	Token string `json:"token"`
}

type searchResponse struct {
	Data []searchItem `json:"data"`
}

type searchItem struct {
	ID         string `json:"id"`
	Attributes struct {
		Language      string `json:"language"`
		Release       string `json:"release"`
		DownloadCount int    `json:"download_count"`
		FromTrusted   bool   `json:"from_trusted"`
		Files         []struct {
			FileID   int64  `json:"file_id"`
			FileName string `json:"file_name"`
		} `json:"files"`
		FeatureDetails struct {
			Title string `json:"title"`
			Year  int    `json:"year"`
		} `json:"feature_details"`
	} `json:"attributes"`
}

type downloadResponse struct {
	Link      string `json:"link"`
	FileName  string `json:"file_name"`
	Remaining int    `json:"remaining"`
	Requests  int    `json:"requests"`
	Message   string `json:"message"`
}

func (c *Client) Search(ctx context.Context, credential map[string]string, input provider.SearchInput) ([]model.SubtitleCandidate, error) {
	apiKey, userAgent, err := credentials(credential)
	if err != nil {
		return nil, err
	}

	token, _ := c.resolveToken(ctx, apiKey, userAgent, credential)

	q := strings.TrimSpace(input.Title)
	if input.MediaType == "episode" && input.Season != nil && input.Episode != nil {
		q = fmt.Sprintf("%s S%02dE%02d", q, *input.Season, *input.Episode)
	}
	if q == "" {
		return nil, fmt.Errorf("empty search query")
	}

	u, err := url.Parse(apiBase + "/subtitles")
	if err != nil {
		return nil, err
	}
	params := u.Query()
	params.Set("query", q)
	params.Set("languages", "zh-cn,zh-tw,zh,en")
	params.Set("order_by", "download_count")
	params.Set("order_direction", "desc")
	if input.Year != nil {
		params.Set("year", strconv.Itoa(*input.Year))
	}
	if input.MediaType == "episode" {
		if input.Season != nil {
			params.Set("season_number", strconv.Itoa(*input.Season))
		}
		if input.Episode != nil {
			params.Set("episode_number", strconv.Itoa(*input.Episode))
		}
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("opensubtitles search failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}
	if len(payload.Data) > limit {
		payload.Data = payload.Data[:limit]
	}

	out := make([]model.SubtitleCandidate, 0, len(payload.Data))
	for _, item := range payload.Data {
		langCode, langDisplay := provider.NormalizeLanguage(item.Attributes.Language)
		title := firstNonEmpty(item.Attributes.FeatureDetails.Title, input.Title)
		candidateID := strings.TrimSpace(item.ID)
		if len(item.Attributes.Files) > 0 && item.Attributes.Files[0].FileID > 0 {
			candidateID = strconv.FormatInt(item.Attributes.Files[0].FileID, 10)
		}

		score := provider.ScoreByLanguage(c.languagePriority, langCode) + float64(item.Attributes.DownloadCount)/200.0
		if item.Attributes.FromTrusted {
			score += 2
		}

		releaseName := item.Attributes.Release
		if releaseName == "" && len(item.Attributes.Files) > 0 {
			releaseName = item.Attributes.Files[0].FileName
		}

		out = append(out, model.SubtitleCandidate{
			MediaItemID:  input.MediaID,
			ProviderName: c.Name(),
			CandidateID:  candidateID,
			Title:        title,
			ReleaseName:  releaseName,
			Language:     langCode,
			LanguageText: langDisplay,
			Score:        score,
			RawPayload:   mustJSON(item),
			Details:      fmt.Sprintf("downloads=%d trusted=%v", item.Attributes.DownloadCount, item.Attributes.FromTrusted),
		})
	}

	return out, nil
}

func (c *Client) Download(ctx context.Context, credential map[string]string, candidate model.SubtitleCandidate) (provider.DownloadResult, error) {
	apiKey, userAgent, err := credentials(credential)
	if err != nil {
		return provider.DownloadResult{}, err
	}
	token, err := c.resolveToken(ctx, apiKey, userAgent, credential)
	if err != nil {
		return provider.DownloadResult{}, err
	}
	if strings.TrimSpace(token) == "" {
		return provider.DownloadResult{}, fmt.Errorf("opensubtitles requires authenticated user for download")
	}

	fileID, err := strconv.ParseInt(strings.TrimSpace(candidate.CandidateID), 10, 64)
	if err != nil || fileID <= 0 {
		return provider.DownloadResult{}, fmt.Errorf("invalid file id: %s", candidate.CandidateID)
	}

	bodyRaw, _ := json.Marshal(map[string]int64{"file_id": fileID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/download", bytes.NewReader(bodyRaw))
	if err != nil {
		return provider.DownloadResult{}, err
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return provider.DownloadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return provider.DownloadResult{}, fmt.Errorf("opensubtitles download request failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload downloadResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return provider.DownloadResult{}, err
	}
	if strings.TrimSpace(payload.Link) == "" {
		if strings.TrimSpace(payload.Message) == "" {
			return provider.DownloadResult{}, fmt.Errorf("opensubtitles download response missing link")
		}
		return provider.DownloadResult{}, fmt.Errorf("opensubtitles download error: %s", payload.Message)
	}

	fileData, err := c.fetchBytes(ctx, payload.Link)
	if err != nil {
		return provider.DownloadResult{}, err
	}

	fileName := strings.TrimSpace(payload.FileName)
	if fileName == "" {
		fileName = firstNonEmpty(candidate.ReleaseName, "opensubtitles_"+strconv.FormatInt(fileID, 10)+".srt")
	}
	fileName = filepath.Base(fileName)

	return provider.DownloadResult{
		Data:     fileData,
		FileName: fileName,
		Note:     fmt.Sprintf("requests=%d remaining=%d", payload.Requests, payload.Remaining),
	}, nil
}

func credentials(credential map[string]string) (apiKey string, userAgent string, err error) {
	apiKey = strings.TrimSpace(credential["api_key"])
	if apiKey == "" {
		return "", "", fmt.Errorf("opensubtitles api_key is empty")
	}
	userAgent = strings.TrimSpace(credential["user_agent"])
	if userAgent == "" {
		userAgent = "4subs v0.1.0"
	}
	return apiKey, userAgent, nil
}

func (c *Client) resolveToken(ctx context.Context, apiKey, userAgent string, credential map[string]string) (string, error) {
	if token := strings.TrimSpace(credential["token"]); token != "" {
		return token, nil
	}
	username := strings.TrimSpace(credential["username"])
	password := strings.TrimSpace(credential["password"])
	if username == "" || password == "" {
		return "", nil
	}
	return c.login(ctx, apiKey, userAgent, username, password)
}

func (c *Client) login(ctx context.Context, apiKey, userAgent, username, password string) (string, error) {
	bodyRaw, _ := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/login", bytes.NewReader(bodyRaw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Api-Key", apiKey)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("opensubtitles login failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.Token) == "" {
		return "", fmt.Errorf("empty login token")
	}
	return payload.Token, nil
}

func (c *Client) fetchBytes(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("opensubtitles file download failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
