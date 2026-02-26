package model

import "time"

type Settings struct {
	LanguagePriority    []string `json:"language_priority"`
	AutoReplaceExisting bool     `json:"auto_replace_existing"`
	SubtitleOutputPath  string   `json:"subtitle_output_path"`
}

type ProviderStatus struct {
	Name           string `json:"name"`
	DisplayName    string `json:"display_name"`
	Configured     bool   `json:"configured"`
	Enabled        bool   `json:"enabled"`
	SupportsSearch bool   `json:"supports_search"`
	SupportsDL     bool   `json:"supports_download"`
	Note           string `json:"note,omitempty"`
}

type Job struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Status    string    `json:"status"`
	Details   string    `json:"details,omitempty"`
	Error     string    `json:"error,omitempty"`
	Retries   int       `json:"retries"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MediaItem struct {
	ID          int64      `json:"id"`
	MediaType   string     `json:"media_type"`
	Title       string     `json:"title"`
	Year        *int       `json:"year,omitempty"`
	Season      *int       `json:"season,omitempty"`
	Episode     *int       `json:"episode,omitempty"`
	FilePath    string     `json:"file_path"`
	MediaHash   string     `json:"media_hash,omitempty"`
	HasSubtitle bool       `json:"has_subtitle"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}
