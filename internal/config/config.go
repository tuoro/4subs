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
	ConfigDir           string
	StaticDir           string
	SubtitleOutputPath  string
	MediaPaths          []string
	AppSecret           string
	ASSRTToken          string
	OpenSubtitlesAPIKey string
	OpenSubtitlesUser   string
	OpenSubtitlesPass   string
	OpenSubtitlesUA     string
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:            envOrDefault("HTTP_ADDR", ":8080"),
		DataDir:             envOrDefault("DATA_DIR", "/app/data"),
		ConfigDir:           envOrDefault("CONFIG_DIR", "/app/config"),
		StaticDir:           envOrDefault("STATIC_DIR", "/app/web/dist"),
		SubtitleOutputPath:  envOrDefault("SUBTITLE_OUTPUT_PATH", "/app/subtitles"),
		MediaPaths:          splitComma(envOrDefault("MEDIA_PATHS", "/media")),
		AppSecret:           os.Getenv("APP_SECRET"),
		ASSRTToken:          os.Getenv("ASSRT_TOKEN"),
		OpenSubtitlesAPIKey: os.Getenv("OPENSUBTITLES_API_KEY"),
		OpenSubtitlesUser:   os.Getenv("OPENSUBTITLES_USERNAME"),
		OpenSubtitlesPass:   os.Getenv("OPENSUBTITLES_PASSWORD"),
		OpenSubtitlesUA:     envOrDefault("OPENSUBTITLES_USER_AGENT", "4subs v0.1.0"),
	}

	cfg.DBPath = envOrDefault("DB_PATH", filepath.Join(cfg.DataDir, "4subs.db"))

	if err := ensureDirs(cfg.DataDir, cfg.ConfigDir, cfg.SubtitleOutputPath); err != nil {
		return Config{}, err
	}

	if cfg.OpenSubtitlesAPIKey == "" {
		// Empty is allowed for bootstrap, but app should show unconfigured state.
	}

	return cfg, nil
}

func ensureDirs(paths ...string) error {
	for _, p := range paths {
		if p == "" {
			return errors.New("directory path is empty")
		}
		if err := os.MkdirAll(p, 0o755); err != nil {
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
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
