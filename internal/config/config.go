package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	HTTPAddr            string
	DBPath              string
	DataDir             string
	WorkDir             string
	ConfigDir           string
	StaticDir           string
	SubtitleOutputPath  string
	MediaPaths          []string
	FFmpegBin           string
	TranslationProvider string
	DeepSeekBaseURL     string
	DeepSeekAPIKey      string
	DeepSeekModel       string
	AppSecret           string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:            envOrDefault("HTTP_ADDR", ":8080"),
		DataDir:             envOrDefault("DATA_DIR", "/app/data"),
		WorkDir:             envOrDefault("WORK_DIR", "/app/work"),
		ConfigDir:           envOrDefault("CONFIG_DIR", "/app/config"),
		StaticDir:           envOrDefault("STATIC_DIR", "/app/web/dist"),
		SubtitleOutputPath:  envOrDefault("SUBTITLE_OUTPUT_PATH", "/app/subtitles"),
		MediaPaths:          splitComma(envOrDefault("MEDIA_PATHS", "/media")),
		FFmpegBin:           envOrDefault("FFMPEG_BIN", "ffmpeg"),
		TranslationProvider: envOrDefault("TRANSLATION_PROVIDER", "deepseek"),
		DeepSeekBaseURL:     envOrDefault("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekAPIKey:      strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY")),
		DeepSeekModel:       envOrDefault("DEEPSEEK_MODEL", "deepseek-chat"),
		AppSecret:           strings.TrimSpace(os.Getenv("APP_SECRET")),
	}

	cfg.DBPath = envOrDefault("DB_PATH", filepath.Join(cfg.DataDir, "4subs.db"))
	if err := ensureDirs(cfg.DataDir, cfg.WorkDir, cfg.ConfigDir, cfg.SubtitleOutputPath); err != nil {
		return Config{}, err
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

