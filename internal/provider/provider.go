package provider

import (
	"context"
	"strings"

	"github.com/gayhub/4subs/internal/model"
)

type SearchInput struct {
	MediaID   int64
	Title     string
	MediaType string
	Year      *int
	Season    *int
	Episode   *int
	FilePath  string
	Limit     int
}

type SearchProvider interface {
	Name() string
	Search(ctx context.Context, credential map[string]string, input SearchInput) ([]model.SubtitleCandidate, error)
}

func NormalizeLanguage(raw string) (code string, display string) {
	r := toLowerTrim(raw)
	switch {
	case containsAny(r, "双语", "bilingual", "chs&eng", "zh-en", "简英", "中英"):
		return "bilingual", raw
	case containsAny(r, "简", "zh-cn", "chs", "simplified"):
		return "zh-cn", raw
	case containsAny(r, "繁", "zh-tw", "cht", "traditional"):
		return "zh-tw", raw
	case containsAny(r, "english", "en"):
		return "en", raw
	default:
		if r == "" {
			return "unknown", ""
		}
		return r, raw
	}
}

func ScoreByLanguage(priority []string, lang string) float64 {
	for i, item := range priority {
		if toLowerTrim(item) == toLowerTrim(lang) {
			return float64(len(priority)-i) * 10
		}
	}
	if toLowerTrim(lang) == "unknown" || toLowerTrim(lang) == "" {
		return 1
	}
	return 3
}

func containsAny(value string, terms ...string) bool {
	for _, t := range terms {
		if t != "" && value != "" && strings.Contains(value, toLowerTrim(t)) {
			return true
		}
	}
	return false
}

func toLowerTrim(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
