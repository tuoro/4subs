package assrt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/model"
	"github.com/gayhub/4subs/internal/provider"
)

const baseURL = "https://api.assrt.net/v1/sub/search"

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

type subItem struct {
	ID         int64  `json:"id"`
	NativeName string `json:"native_name"`
	VideoName  string `json:"videoname"`
	Lang       struct {
		Desc string `json:"desc"`
	} `json:"lang"`
	VoteScore float64 `json:"vote_score"`
	Detail    string  `json:"detail"`
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

	u, err := url.Parse(baseURL)
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
