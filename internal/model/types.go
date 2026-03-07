package model

import "time"

type AppSettings struct {
	MediaPaths          []string  `json:"media_paths"`
	SourceLanguage      string    `json:"source_language"`
	TargetLanguage      string    `json:"target_language"`
	BilingualLayout     string    `json:"bilingual_layout"`
	OutputFormats       []string  `json:"output_formats"`
	TranslationProvider string    `json:"translation_provider"`
	TranslationModel    string    `json:"translation_model"`
	TranslationPrompt   string    `json:"translation_prompt"`
	MaxSubtitlePerBatch int       `json:"max_subtitle_per_batch"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type MediaAsset struct {
	ID           int64     `json:"id"`
	Title        string    `json:"title"`
	RootPath     string    `json:"root_path"`
	RelativePath string    `json:"relative_path"`
	FilePath     string    `json:"file_path"`
	FileSize     int64     `json:"file_size"`
	Status       string    `json:"status"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SubtitleJob struct {
	ID                 string    `json:"id"`
	MediaAssetID       *int64    `json:"media_asset_id,omitempty"`
	MediaPath          string    `json:"media_path"`
	FileName           string    `json:"file_name"`
	Status             string    `json:"status"`
	CurrentStage       string    `json:"current_stage"`
	Progress           int       `json:"progress"`
	SourceLanguage     string    `json:"source_language"`
	TargetLanguage     string    `json:"target_language"`
	Provider           string    `json:"provider"`
	OutputFormats      []string  `json:"output_formats"`
	SourceSubtitlePath string    `json:"source_subtitle_path,omitempty"`
	OutputSubtitlePath string    `json:"output_subtitle_path,omitempty"`
	Details            string    `json:"details,omitempty"`
	ErrorMessage       string    `json:"error_message,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type PipelineStep struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Owner       string `json:"owner"`
}

type Overview struct {
	AppName          string         `json:"app_name"`
	AppSummary       string         `json:"app_summary"`
	TranslationReady bool           `json:"translation_ready"`
	AsrReady         bool           `json:"asr_ready"`
	MediaAssetCount  int            `json:"media_asset_count"`
	PendingJobCount  int            `json:"pending_job_count"`
	RecentJobs       []SubtitleJob  `json:"recent_jobs"`
	Pipeline         []PipelineStep `json:"pipeline"`
	CurrentSettings  AppSettings    `json:"current_settings"`
}
