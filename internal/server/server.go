package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/db"
	"github.com/gayhub/4subs/internal/library"
	"github.com/gayhub/4subs/internal/model"
	"github.com/gayhub/4subs/internal/pipeline"
	"github.com/gayhub/4subs/internal/translator/deepseek"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	cfg        config.Config
	repo       *db.Repository
	translator deepseek.Client
}

type createJobRequest struct {
	MediaAssetID   *int64   `json:"media_asset_id"`
	MediaPath      string   `json:"media_path"`
	FileName       string   `json:"file_name"`
	SourceLanguage string   `json:"source_language"`
	TargetLanguage string   `json:"target_language"`
	OutputFormats  []string `json:"output_formats"`
	Details        string   `json:"details"`
}

func New(cfg config.Config, repo *db.Repository) *Server {
	return &Server{
		cfg:  cfg,
		repo: repo,
		translator: deepseek.Client{
			BaseURL: cfg.DeepSeekBaseURL,
			APIKey:  cfg.DeepSeekAPIKey,
			Model:   cfg.DeepSeekModel,
		},
	}
}

func (s *Server) Routes() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(60 * time.Second))

	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", s.handleHealth)
		api.Get("/overview", s.handleOverview)
		api.Get("/pipeline", s.handlePipeline)
		api.Get("/settings", s.handleGetSettings)
		api.Put("/settings", s.handleSaveSettings)
		api.Get("/media", s.handleListMedia)
		api.Post("/media/scan", s.handleScanMedia)
		api.Get("/jobs", s.handleListJobs)
		api.Post("/jobs", s.handleCreateJob)
	})

	staticDir := strings.TrimSpace(s.cfg.StaticDir)
	if staticDir == "" {
		return router
	}

	fileServer := http.FileServer(http.Dir(staticDir))
	router.Handle("/assets/*", fileServer)
	router.Get("/*", func(writer http.ResponseWriter, request *http.Request) {
		requestPath := strings.TrimPrefix(request.URL.Path, "/")
		if requestPath == "" {
			http.ServeFile(writer, request, filepath.Join(staticDir, "index.html"))
			return
		}
		targetPath := filepath.Join(staticDir, filepath.FromSlash(requestPath))
		if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(writer, request)
			return
		}
		http.ServeFile(writer, request, filepath.Join(staticDir, "index.html"))
	})

	return router
}

func (s *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	s.writeJSON(writer, http.StatusOK, map[string]any{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
	})
}

func (s *Server) handleOverview(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	mediaCount, err := s.repo.CountMediaAssets(ctx)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	pendingCount, err := s.repo.CountPendingJobs(ctx)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	recentJobs, err := s.repo.ListJobs(ctx, 8)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	overview := model.Overview{
		AppName:          "4subs",
		AppSummary:       "面向本地媒体的双语字幕生成平台，使用 Go 后端、PrimeVue 前端和 DeepSeek 翻译能力。",
		TranslationReady: s.translator.Ready(),
		MediaAssetCount:  mediaCount,
		PendingJobCount:  pendingCount,
		RecentJobs:       recentJobs,
		Pipeline:         pipeline.DefaultSteps(),
		CurrentSettings:  settings,
	}
	s.writeJSON(writer, http.StatusOK, overview)
}

func (s *Server) handlePipeline(writer http.ResponseWriter, request *http.Request) {
	s.writeJSON(writer, http.StatusOK, map[string]any{
		"steps": pipeline.DefaultSteps(),
		"runtime": map[string]any{
			"ffmpeg_bin":          s.cfg.FFmpegBin,
			"work_dir":            s.cfg.WorkDir,
			"subtitle_output_dir": s.cfg.SubtitleOutputPath,
			"translation_ready":   s.translator.Ready(),
		},
	})
}

func (s *Server) handleGetSettings(writer http.ResponseWriter, request *http.Request) {
	settings, err := s.repo.GetSettings(request.Context())
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, settings)
}

func (s *Server) handleSaveSettings(writer http.ResponseWriter, request *http.Request) {
	var settings model.AppSettings
	if err := json.NewDecoder(request.Body).Decode(&settings); err != nil {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("请求体解析失败: %w", err))
		return
	}
	if err := s.repo.SaveSettings(request.Context(), normalizeSettings(settings)); err != nil {
		s.writeError(writer, http.StatusBadRequest, err)
		return
	}
	fresh, err := s.repo.GetSettings(request.Context())
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, fresh)
}

func (s *Server) handleListMedia(writer http.ResponseWriter, request *http.Request) {
	limit := parseLimit(request.URL.Query().Get("limit"), 200)
	assets, err := s.repo.ListMediaAssets(request.Context(), limit)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, map[string]any{"items": assets})
}

func (s *Server) handleScanMedia(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	assets, err := library.ScanMediaPaths(settings.MediaPaths)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	if err := s.repo.ReplaceMediaAssets(ctx, assets); err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, map[string]any{
		"count": len(assets),
		"items": assets,
	})
}

func (s *Server) handleListJobs(writer http.ResponseWriter, request *http.Request) {
	limit := parseLimit(request.URL.Query().Get("limit"), 100)
	jobs, err := s.repo.ListJobs(request.Context(), limit)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, map[string]any{"items": jobs})
}

func (s *Server) handleCreateJob(writer http.ResponseWriter, request *http.Request) {
	var payload createJobRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("请求体解析失败: %w", err))
		return
	}
	settings, err := s.repo.GetSettings(request.Context())
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(payload.MediaPath) == "" && payload.MediaAssetID != nil {
		assets, err := s.repo.ListMediaAssets(request.Context(), 500)
		if err != nil {
			s.writeError(writer, http.StatusInternalServerError, err)
			return
		}
		for _, asset := range assets {
			if asset.ID == *payload.MediaAssetID {
				payload.MediaPath = asset.FilePath
				payload.FileName = filepath.Base(asset.FilePath)
				break
			}
		}
	}
	job, err := s.repo.CreateJob(request.Context(), db.CreateJobInput{
		MediaAssetID:   payload.MediaAssetID,
		MediaPath:      payload.MediaPath,
		FileName:       firstNonEmpty(payload.FileName, filepath.Base(payload.MediaPath)),
		SourceLanguage: firstNonEmpty(payload.SourceLanguage, settings.SourceLanguage),
		TargetLanguage: firstNonEmpty(payload.TargetLanguage, settings.TargetLanguage),
		Provider:       settings.TranslationProvider,
		OutputFormats:  normalizeFormats(payload.OutputFormats, settings.OutputFormats),
		Details:        firstNonEmpty(payload.Details, "任务已创建，等待后续接入完整字幕处理流水线。"),
	})
	if err != nil {
		s.writeError(writer, http.StatusBadRequest, err)
		return
	}
	s.writeJSON(writer, http.StatusCreated, job)
}

func (s *Server) writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func (s *Server) writeError(writer http.ResponseWriter, status int, err error) {
	message := "请求失败"
	if err != nil {
		message = err.Error()
	}
	s.writeJSON(writer, status, map[string]any{
		"error": message,
	})
}

func normalizeSettings(settings model.AppSettings) model.AppSettings {
	settings.MediaPaths = trimNonEmpty(settings.MediaPaths)
	settings.OutputFormats = normalizeFormats(settings.OutputFormats, []string{"srt"})
	settings.SourceLanguage = firstNonEmpty(settings.SourceLanguage, "auto")
	settings.TargetLanguage = firstNonEmpty(settings.TargetLanguage, "zh-CN")
	settings.BilingualLayout = firstNonEmpty(settings.BilingualLayout, "origin_above")
	settings.TranslationProvider = firstNonEmpty(settings.TranslationProvider, "deepseek")
	settings.TranslationModel = firstNonEmpty(settings.TranslationModel, "deepseek-chat")
	if settings.MaxSubtitlePerBatch <= 0 {
		settings.MaxSubtitlePerBatch = 30
	}
	return settings
}

func trimNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizeFormats(values []string, fallback []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseLimit(raw string, fallback int) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return fallback
	}
	if limit > 500 {
		return 500
	}
	return limit
}

