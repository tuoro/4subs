package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	openaiasr "github.com/gayhub/4subs/internal/asr/openai"
	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/db"
	"github.com/gayhub/4subs/internal/joblog"
	"github.com/gayhub/4subs/internal/jobrunner"
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
	asr        openaiasr.Client
	runner     *jobrunner.Runner
	logger     *joblog.Store
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

type previewResponse struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Editable bool   `json:"editable"`
	Content  string `json:"content"`
}

type previewSaveRequest struct {
	Content string `json:"content"`
}

func New(cfg config.Config, repo *db.Repository) *Server {
	translatorClient := deepseek.Client{BaseURL: cfg.DeepSeekBaseURL, APIKey: cfg.DeepSeekAPIKey, Model: cfg.DeepSeekModel}
	asrClient := openaiasr.Client{BaseURL: cfg.ASRBaseURL, APIKey: cfg.ASRAPIKey, Model: cfg.ASRModel}
	logger := joblog.New(cfg.WorkDir)
	runner := jobrunner.New(cfg, repo, translatorClient, asrClient, logger)
	runner.ResumePending(context.Background())
	return &Server{cfg: cfg, repo: repo, translator: translatorClient, asr: asrClient, runner: runner, logger: logger}
}

func (s *Server) Routes() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.RealIP)
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(120 * time.Second))

	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", s.handleHealth)
		api.Get("/overview", s.handleOverview)
		api.Get("/pipeline", s.handlePipeline)
		api.Get("/settings", s.handleGetSettings)
		api.Put("/settings", s.handleSaveSettings)
		api.Get("/media", s.handleListMedia)
		api.Post("/media/scan", s.handleScanMedia)
		api.Get("/jobs", s.handleListJobs)
		api.Get("/jobs/{id}", s.handleGetJob)
		api.Get("/jobs/{id}/logs", s.handleGetJobLogs)
		api.Post("/jobs", s.handleCreateJob)
		api.Post("/jobs/{id}/retry", s.handleRetryJob)
		api.Post("/jobs/{id}/cancel", s.handleCancelJob)
		api.Get("/jobs/{id}/download", s.handleDownloadJobResult)
		api.Get("/jobs/{id}/preview", s.handleGetJobPreview)
		api.Put("/jobs/{id}/preview", s.handleSaveJobPreview)
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
		"status":               "ok",
		"timestamp":            time.Now().UTC(),
		"translation_ready":    s.translator.Ready(),
		"asr_ready":            s.asr.Ready(),
		"job_concurrency":      s.cfg.JobConcurrency,
		"subtitle_output_path": s.cfg.SubtitleOutputPath,
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
	response := model.Overview{
		AppName:           "4subs",
		AppSummary:        "当前版本已支持 SRT/ASS 双格式输出、在线校对、并发任务执行、任务取消与任务日志追踪。",
		TranslationReady:  s.translator.Ready(),
		AsrReady:          s.asr.Ready(),
		WorkerConcurrency: s.cfg.JobConcurrency,
		MediaAssetCount:   mediaCount,
		PendingJobCount:   pendingCount,
		RecentJobs:        recentJobs,
		Pipeline:          pipeline.DefaultSteps(),
		CurrentSettings:   settings,
	}
	s.writeJSON(writer, http.StatusOK, response)
}

func (s *Server) handlePipeline(writer http.ResponseWriter, request *http.Request) {
	s.writeJSON(writer, http.StatusOK, map[string]any{
		"steps": pipeline.DefaultSteps(),
		"runtime": map[string]any{
			"ffmpeg_bin":           s.cfg.FFmpegBin,
			"work_dir":             s.cfg.WorkDir,
			"subtitle_output_dir":  s.cfg.SubtitleOutputPath,
			"translation_provider": s.cfg.TranslationProvider,
			"translation_ready":    s.translator.Ready(),
			"asr_provider":         s.cfg.ASRProvider,
			"asr_model":            s.cfg.ASRModel,
			"asr_ready":            s.asr.Ready(),
			"job_concurrency":      s.cfg.JobConcurrency,
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
	assets, err := s.repo.ListMediaAssets(request.Context(), parseLimit(request.URL.Query().Get("limit"), 200))
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
	s.writeJSON(writer, http.StatusOK, map[string]any{"count": len(assets), "items": assets})
}

func (s *Server) handleListJobs(writer http.ResponseWriter, request *http.Request) {
	jobs, err := s.repo.ListJobs(request.Context(), parseLimit(request.URL.Query().Get("limit"), 100))
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, map[string]any{"items": jobs})
}

func (s *Server) handleGetJob(writer http.ResponseWriter, request *http.Request) {
	job, err := s.repo.GetJob(request.Context(), chi.URLParam(request, "id"))
	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, job)
}

func (s *Server) handleGetJobLogs(writer http.ResponseWriter, request *http.Request) {
	jobID := chi.URLParam(request, "id")
	if _, err := s.repo.GetJob(request.Context(), jobID); err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	entries, err := s.logger.List(jobID, parseLimit(request.URL.Query().Get("limit"), 200))
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, map[string]any{"items": entries})
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
	if payload.MediaAssetID != nil {
		asset, err := s.repo.GetMediaAsset(request.Context(), *payload.MediaAssetID)
		if err != nil {
			if err == sql.ErrNoRows {
				s.writeError(writer, http.StatusNotFound, fmt.Errorf("媒体不存在"))
				return
			}
			s.writeError(writer, http.StatusInternalServerError, err)
			return
		}
		payload.MediaPath = asset.FilePath
		payload.FileName = firstNonEmpty(payload.FileName, asset.RelativePath, filepath.Base(asset.FilePath))
	}
	job, err := s.repo.CreateJob(request.Context(), db.CreateJobInput{
		MediaAssetID:   payload.MediaAssetID,
		MediaPath:      payload.MediaPath,
		FileName:       firstNonEmpty(payload.FileName, filepath.Base(payload.MediaPath)),
		SourceLanguage: firstNonEmpty(payload.SourceLanguage, settings.SourceLanguage),
		TargetLanguage: firstNonEmpty(payload.TargetLanguage, settings.TargetLanguage),
		Provider:       settings.TranslationProvider,
		OutputFormats:  normalizeFormats(payload.OutputFormats, settings.OutputFormats),
		Details:        firstNonEmpty(payload.Details, "任务已创建，后台会先找字幕，找不到再自动转为 ASR。"),
	})
	if err != nil {
		s.writeError(writer, http.StatusBadRequest, err)
		return
	}
	_ = s.logger.Append(job.ID, "info", "queued", "任务已创建，等待进入执行队列", "")
	s.runner.Enqueue(job.ID)
	s.writeJSON(writer, http.StatusCreated, job)
}

func (s *Server) handleRetryJob(writer http.ResponseWriter, request *http.Request) {
	jobID := chi.URLParam(request, "id")
	job, err := s.repo.GetJob(request.Context(), jobID)
	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	if job.Status != "failed" && job.Status != "cancelled" {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("只有失败或已取消的任务才能重试"))
		return
	}
	paths := db.JobOutputPaths{}
	if err := s.repo.UpdateJobProgress(request.Context(), job.ID, "queued", "queued", 0, "任务已重新排队", paths, ""); err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	_ = s.logger.Append(job.ID, "warn", "queued", "任务已重新排队", "")
	s.runner.Enqueue(job.ID)
	fresh, err := s.repo.GetJob(request.Context(), job.ID)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, fresh)
}

func (s *Server) handleCancelJob(writer http.ResponseWriter, request *http.Request) {
	jobID := chi.URLParam(request, "id")
	if err := s.runner.Cancel(jobID); err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusBadRequest, err)
		return
	}
	job, err := s.repo.GetJob(request.Context(), jobID)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	_ = s.logger.Append(jobID, "warn", job.CurrentStage, "已收到取消请求", "")
	s.writeJSON(writer, http.StatusOK, job)
}

func (s *Server) handleDownloadJobResult(writer http.ResponseWriter, request *http.Request) {
	job, err := s.repo.GetJob(request.Context(), chi.URLParam(request, "id"))
	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	targetPath := strings.TrimSpace(job.OutputSubtitlePath)
	switch strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind"))) {
	case "srt":
		targetPath = strings.TrimSpace(job.OutputSRTPath)
	case "ass":
		targetPath = strings.TrimSpace(job.OutputASSPath)
	case "", "output":
	default:
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("不支持的下载类型"))
		return
	}
	if targetPath == "" {
		s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务尚未生成对应字幕"))
		return
	}
	if _, err := os.Stat(targetPath); err != nil {
		s.writeError(writer, http.StatusNotFound, fmt.Errorf("输出字幕文件不存在"))
		return
	}
	writer.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(targetPath)))
	http.ServeFile(writer, request, targetPath)
}

func (s *Server) handleGetJobPreview(writer http.ResponseWriter, request *http.Request) {
	job, err := s.repo.GetJob(request.Context(), chi.URLParam(request, "id"))
	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind")))
	if kind == "" {
		kind = "output"
	}
	payload, err := s.readPreview(job, kind)
	if err != nil {
		s.writeError(writer, http.StatusBadRequest, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, payload)
}

func (s *Server) handleSaveJobPreview(writer http.ResponseWriter, request *http.Request) {
	job, err := s.repo.GetJob(request.Context(), chi.URLParam(request, "id"))
	if err != nil {
		if err == sql.ErrNoRows {
			s.writeError(writer, http.StatusNotFound, fmt.Errorf("任务不存在"))
			return
		}
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	kind := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("kind")))
	if kind == "" {
		kind = "output"
	}
	var targetPath string
	switch kind {
	case "output", "srt":
		targetPath = strings.TrimSpace(job.OutputSRTPath)
		kind = "srt"
	case "ass":
		targetPath = strings.TrimSpace(job.OutputASSPath)
	default:
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("该类型字幕不可编辑"))
		return
	}
	if targetPath == "" {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("任务还没有可编辑的输出字幕"))
		return
	}
	var payload previewSaveRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("请求体解析失败: %w", err))
		return
	}
	content := strings.ReplaceAll(payload.Content, "\r\n", "\n")
	if strings.TrimSpace(content) == "" {
		s.writeError(writer, http.StatusBadRequest, fmt.Errorf("字幕内容不能为空"))
		return
	}
	if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	paths := db.JobOutputPaths{
		SourcePath:  job.SourceSubtitlePath,
		PrimaryPath: choosePrimaryOutputPath(job.OutputFormats, job.OutputSRTPath, job.OutputASSPath),
		SRTPath:     job.OutputSRTPath,
		ASSPath:     job.OutputASSPath,
	}
	if kind == "srt" {
		paths.SRTPath = targetPath
		paths.PrimaryPath = choosePrimaryOutputPath(job.OutputFormats, targetPath, job.OutputASSPath)
		job.OutputSRTPath = targetPath
		job.OutputSubtitlePath = paths.PrimaryPath
	}
	if kind == "ass" {
		paths.ASSPath = targetPath
		paths.PrimaryPath = choosePrimaryOutputPath(job.OutputFormats, job.OutputSRTPath, targetPath)
		job.OutputASSPath = targetPath
		job.OutputSubtitlePath = paths.PrimaryPath
	}
	if err := s.repo.UpdateJobProgress(request.Context(), job.ID, job.Status, job.CurrentStage, job.Progress, fmt.Sprintf("%s 字幕已人工保存", strings.ToUpper(kind)), paths, ""); err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	_ = s.logger.Append(job.ID, "info", "review", fmt.Sprintf("%s 字幕已人工保存", strings.ToUpper(kind)), targetPath)
	fresh, err := s.readPreview(job, kind)
	if err != nil {
		s.writeError(writer, http.StatusInternalServerError, err)
		return
	}
	s.writeJSON(writer, http.StatusOK, fresh)
}

func (s *Server) readPreview(job model.SubtitleJob, kind string) (previewResponse, error) {
	preview := previewResponse{Kind: kind}
	var targetPath string
	switch kind {
	case "source":
		targetPath = strings.TrimSpace(job.SourceSubtitlePath)
		preview.Editable = false
	case "srt":
		targetPath = strings.TrimSpace(job.OutputSRTPath)
		preview.Editable = true
	case "ass":
		targetPath = strings.TrimSpace(job.OutputASSPath)
		preview.Editable = true
	case "output":
		targetPath = strings.TrimSpace(job.OutputSubtitlePath)
		preview.Editable = true
	default:
		return previewResponse{}, fmt.Errorf("不支持的预览类型: %s", kind)
	}
	preview.Path = targetPath
	if targetPath == "" {
		return preview, nil
	}
	raw, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return preview, nil
		}
		return previewResponse{}, err
	}
	preview.Exists = true
	preview.Content = string(raw)
	return preview, nil
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
	s.writeJSON(writer, status, map[string]any{"error": message})
}

func normalizeSettings(settings model.AppSettings) model.AppSettings {
	settings.MediaPaths = trimNonEmpty(settings.MediaPaths)
	settings.OutputFormats = normalizeFormats(settings.OutputFormats, []string{"srt", "ass"})
	settings.SourceLanguage = firstNonEmpty(settings.SourceLanguage, "auto")
	settings.TargetLanguage = firstNonEmpty(settings.TargetLanguage, "zh-CN")
	settings.BilingualLayout = firstNonEmpty(settings.BilingualLayout, "origin_above")
	settings.TranslationProvider = firstNonEmpty(settings.TranslationProvider, "deepseek")
	settings.TranslationModel = firstNonEmpty(settings.TranslationModel, "deepseek-chat")
	settings.TranslationPrompt = firstNonEmpty(settings.TranslationPrompt, "请逐条翻译字幕文本，只输出目标语言译文，不要解释，不要合并或拆分字幕。")
	if settings.MaxSubtitlePerBatch <= 0 {
		settings.MaxSubtitlePerBatch = 20
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
		if trimmed != "srt" && trimmed != "ass" {
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

func choosePrimaryOutputPath(formats []string, srtPath string, assPath string) string {
	for _, format := range formats {
		trimmed := strings.ToLower(strings.TrimSpace(format))
		if trimmed == "srt" && strings.TrimSpace(srtPath) != "" {
			return srtPath
		}
		if trimmed == "ass" && strings.TrimSpace(assPath) != "" {
			return assPath
		}
	}
	if strings.TrimSpace(srtPath) != "" {
		return srtPath
	}
	return assPath
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
