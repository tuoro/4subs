package assrt

import (
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

const (
	searchURL = "https://api.assrt.net/v1/sub/search"
	detailURL = "https://api.assrt.net/v1/sub/detail"
)

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
	return "assrt"
}

type searchResponse struct {
	Status int `json:"status"`
	Sub    struct {
		Subs []subItem `json:"subs"`
	} `json:"sub"`
}

type detailResponse struct {
	Status int `json:"status"`
	Sub    struct {
		Subs []subDetailItem `json:"subs"`
	} `json:"sub"`
}

type subItem struct {
	ID         int64   `json:"id"`
	NativeName string  `json:"native_name"`
	VideoName  string  `json:"videoname"`
	Lang       langObj `json:"lang"`
	VoteScore  float64 `json:"vote_score"`
	Detail     string  `json:"detail"`
}

type subDetailItem struct {
	ID         int64   `json:"id"`
	Filename   string  `json:"filename"`
	NativeName string  `json:"native_name"`
	VideoName  string  `json:"videoname"`
	Lang       langObj `json:"lang"`
	VoteScore  float64 `json:"vote_score"`
	URL        string  `json:"url"`
	FileList   []struct {
		URL string `json:"url"`
		F   string `json:"f"`
	} `json:"filelist"`
}

type langObj struct {
	Desc string `json:"desc"`
}

func (c *Client) Search(ctx context.Context, credential map[string]string, input provider.SearchInput) ([]model.SubtitleCandidate, error) {
	token := strings.TrimSpace(credential["token"])
	if token == "" {
		return nil, fmt.Errorf("assrt token is empty")
	}

	q := strings.TrimSpace(input.Title)
	if input.MediaType == "episode" && input.Season != nil && input.Episode != nil {
		q = fmt.Sprintf("%s S%02dE%02d", q, *input.Season, *input.Episode)
	}
	if input.Year != nil {
		q = fmt.Sprintf("%s %d", q, *input.Year)
	}
	if q == "" {
		return nil, fmt.Errorf("empty search query")
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	u, err := url.Parse(searchURL)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	query.Set("token", token)
	query.Set("q", q)
	query.Set("cnt", strconv.Itoa(limit))
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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
		return nil, fmt.Errorf("assrt search failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != 0 {
		return nil, fmt.Errorf("assrt status %d", payload.Status)
	}

	out := make([]model.SubtitleCandidate, 0, len(payload.Sub.Subs))
	for _, item := range payload.Sub.Subs {
		langCode, langDisplay := provider.NormalizeLanguage(item.Lang.Desc)
		score := provider.ScoreByLanguage(c.languagePriority, langCode) + item.VoteScore
		out = append(out, model.SubtitleCandidate{
			MediaItemID:  input.MediaID,
			ProviderName: c.Name(),
			CandidateID:  strconv.FormatInt(item.ID, 10),
			Title:        firstNonEmpty(item.NativeName, item.VideoName, input.Title),
			ReleaseName:  item.VideoName,
			Language:     langCode,
			LanguageText: langDisplay,
			Score:        score,
			Details:      item.Detail,
			RawPayload:   mustJSON(item),
		})
	}
	return out, nil
}

func (c *Client) Download(ctx context.Context, credential map[string]string, candidate model.SubtitleCandidate) (provider.DownloadResult, error) {
	token := strings.TrimSpace(credential["token"])
	if token == "" {
		return provider.DownloadResult{}, fmt.Errorf("assrt token is empty")
	}
	if strings.TrimSpace(candidate.CandidateID) == "" {
		return provider.DownloadResult{}, fmt.Errorf("candidate id is empty")
	}

	detail, err := c.fetchDetail(ctx, token, candidate.CandidateID)
	if err != nil {
		return provider.DownloadResult{}, err
	}
	if len(detail.Sub.Subs) == 0 {
		return provider.DownloadResult{}, fmt.Errorf("no subtitle detail found")
	}

	item := detail.Sub.Subs[0]
	downloadURL := strings.TrimSpace(item.URL)
	fileName := strings.TrimSpace(item.Filename)
	for _, file := range item.FileList {
		if strings.TrimSpace(file.URL) == "" {
			continue
		}
		downloadURL = strings.TrimSpace(file.URL)
		if strings.TrimSpace(file.F) != "" {
			fileName = strings.TrimSpace(file.F)
		}
		break
	}
	if downloadURL == "" {
		return provider.DownloadResult{}, fmt.Errorf("assrt detail missing download url")
	}

	data, err := c.fetchBytes(ctx, downloadURL)
	if err != nil {
		return provider.DownloadResult{}, err
	}

	if fileName == "" {
		fileName = "assrt_" + candidate.CandidateID + ".srt"
	}
	fileName = filepath.Base(fileName)

	return provider.DownloadResult{
		Data:     data,
		FileName: fileName,
		Note:     "assrt detail download",
	}, nil
}

func (c *Client) fetchDetail(ctx context.Context, token string, subtitleID string) (detailResponse, error) {
	u, err := url.Parse(detailURL)
	if err != nil {
		return detailResponse{}, err
	}
	q := u.Query()
	q.Set("token", token)
	q.Set("id", strings.TrimSpace(subtitleID))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return detailResponse{}, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return detailResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return detailResponse{}, fmt.Errorf("assrt detail failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload detailResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return detailResponse{}, err
	}
	if payload.Status != 0 {
		return detailResponse{}, fmt.Errorf("assrt detail status %d", payload.Status)
	}
	return payload, nil
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
		return nil, fmt.Errorf("assrt file download failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
