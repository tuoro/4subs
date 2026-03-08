package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr             string
	DBPath               string
	DataDir              string
	WorkDir              string
	ConfigDir            string
	StaticDir            string
	SubtitleOutputPath   string
	MediaPaths           []string
	FFmpegBin            string
	TranslationProvider  string
	DeepSeekBaseURL      string
	DeepSeekAPIKey       string
	DeepSeekModel        string
	ASRProvider          string
	ASRBaseURL           string
	ASRAPIKey            string
	ASRModel             string
	OCRProvider          string
	OCRBaseURL           string
	OCRAPIKey            string
	OCRModel             string
	OCRFrameIntervalMS   int
	OCRCropTopPercent    int
	OCRCropHeightPercent int
	JobConcurrency       int
	AppSecret            string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:             envOrDefault("HTTP_ADDR", ":8080"),
		DataDir:              envOrDefault("DATA_DIR", "/app/data"),
		WorkDir:              envOrDefault("WORK_DIR", "/app/work"),
		ConfigDir:            envOrDefault("CONFIG_DIR", "/app/config"),
		StaticDir:            envOrDefault("STATIC_DIR", "/app/web/dist"),
		SubtitleOutputPath:   envOrDefault("SUBTITLE_OUTPUT_PATH", "/app/subtitles"),
		MediaPaths:           splitComma(envOrDefault("MEDIA_PATHS", "/media")),
		FFmpegBin:            envOrDefault("FFMPEG_BIN", "ffmpeg"),
		TranslationProvider:  envOrDefault("TRANSLATION_PROVIDER", "deepseek"),
		DeepSeekBaseURL:      envOrDefault("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekAPIKey:       strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		DeepSeekModel:        envOrDefault("DEEPSEEK_MODEL", "deepseek-chat"),
		ASRProvider:          envOrDefault("ASR_PROVIDER", "openai-compatible"),
		ASRBaseURL:           envOrDefault("ASR_BASE_URL", "https://api.openai.com/v1"),
		ASRAPIKey:            strings.TrimSpace(os.Getenv("ASR_API_KEY")),
		ASRModel:             envOrDefault("ASR_MODEL", "whisper-1"),
		OCRProvider:          envOrDefault("OCR_PROVIDER", "openai-compatible-vision"),
		OCRBaseURL:           envOrDefault("OCR_BASE_URL", "https://api.openai.com/v1"),
		OCRAPIKey:            strings.TrimSpace(os.Getenv("OCR_API_KEY")),
		OCRModel:             envOrDefault("OCR_MODEL", "gpt-4.1-mini"),
		OCRFrameIntervalMS:   intEnvOrDefault("OCR_FRAME_INTERVAL_MS", 1000),
		OCRCropTopPercent:    intEnvOrDefault("OCR_CROP_TOP_PERCENT", 72),
		OCRCropHeightPercent: intEnvOrDefault("OCR_CROP_HEIGHT_PERCENT", 22),
		JobConcurrency:       intEnvOrDefault("JOB_CONCURRENCY", 2),
		AppSecret:            strings.TrimSpace(os.Getenv("APP_SECRET")),
	}

	cfg.DBPath = envOrDefault("DB_PATH", filepath.Join(cfg.DataDir, "4subs.db"))
	if err := ensureDirs(cfg.DataDir, cfg.WorkDir, cfg.ConfigDir, cfg.SubtitleOutputPath); err != nil {
		return Config{}, err
	}
	if cfg.JobConcurrency <= 0 {
		cfg.JobConcurrency = 1
	}
	if cfg.OCRFrameIntervalMS <= 0 {
		cfg.OCRFrameIntervalMS = 1000
	}
	if cfg.OCRCropTopPercent < 0 || cfg.OCRCropTopPercent >= 100 {
		cfg.OCRCropTopPercent = 72
	}
	if cfg.OCRCropHeightPercent <= 0 || cfg.OCRCropHeightPercent > 100 {
		cfg.OCRCropHeightPercent = 22
	}
	return cfg, nil
}

func ensureDirs(paths ...string) error {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			return errors.New("目录路径不能为空")
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func splitComma(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func intEnvOrDefault(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
