package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/db"
	"github.com/gayhub/4subs/internal/model"
	"github.com/gayhub/4subs/internal/scanner"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	cfg    config.Config
	repo   *db.Repository
	events *EventBus
}

func New(cfg config.Config, repo *db.Repository) *Server {
	return &Server{
		cfg:    cfg,
		repo:   repo,
		events: NewEventBus(),
	}
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", s.handleHealth)
		api.Get("/settings", s.handleGetSettings)
		api.Put("/settings", s.handleUpdateSettings)
		api.Get("/providers", s.handleListProviders)
		api.Put("/providers/{name}/credential", s.handleSaveCredential)
		api.Get("/jobs", s.handleJobs)
		api.Post("/scan", s.handleScan)
		api.Get("/events", s.handleEvents)
		api.Get("/media", s.handleMedia)
	})

	// Serve built PrimeVue app if present.
	r.Handle("/*", s.staticHandler())
	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"service":      "4subs",
		"version":      "0.1.0",
		"time":         time.Now().UTC().Format(time.RFC3339),
		"storage":      "sqlite",
		"runtime_mode": "docker-first",
	})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.repo.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var payload model.Settings
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}
	if len(payload.LanguagePriority) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("language_priority cannot be empty"))
		return
	}
	if strings.TrimSpace(payload.SubtitleOutputPath) == "" {
		writeError(w, http.StatusBadRequest, errors.New("subtitle_output_path cannot be empty"))
		return
	}

	if err := s.repo.UpdateSettings(r.Context(), payload); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.events.Publish("settings.updated", payload)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.repo.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleSaveCredential(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "name")))
	if name != "assrt" && name != "opensubtitles" {
		writeError(w, http.StatusBadRequest, errors.New("provider must be assrt or opensubtitles"))
		return
	}

	var payload map[string]string
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	trimmed := make(map[string]string, len(payload))
	for k, v := range payload {
		if strings.TrimSpace(v) == "" {
			continue
		}
		trimmed[k] = strings.TrimSpace(v)
	}
	if len(trimmed) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("no non-empty credential fields provided"))
		return
	}

	blob, err := json.Marshal(trimmed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	encrypted, err := encrypt(blob, s.cfg.AppSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.repo.SaveProviderCredential(r.Context(), name, encrypted); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	s.events.Publish("provider.credential_saved", map[string]string{"provider": name})
	writeJSON(w, http.StatusOK, map[string]any{"provider": name, "configured": true})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	jobs, err := s.repo.ListJobs(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	job, err := s.repo.CreateJob(r.Context(), "scan", "Scan media library for missing subtitles")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.events.Publish("job.created", job)

	go s.runScanJob(job.ID)
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) runScanJob(jobID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = s.repo.UpdateJob(ctx, jobID, "running", "", "")
	s.events.Publish("job.updated", map[string]string{"id": jobID, "status": "running"})
	scanResult, err := scanner.Run(s.cfg.MediaPaths)
	if err != nil {
		_ = s.repo.UpdateJob(ctx, jobID, "failed", "", err.Error())
		s.events.Publish("job.updated", map[string]string{"id": jobID, "status": "failed", "error": err.Error()})
		return
	}

	inserted, updated, err := s.repo.UpsertMediaItems(ctx, scanResult.Items)
	if err != nil {
		_ = s.repo.UpdateJob(ctx, jobID, "failed", "", err.Error())
		s.events.Publish("job.updated", map[string]string{"id": jobID, "status": "failed", "error": err.Error()})
		return
	}

	details := fmt.Sprintf(
		"Scanned %d video files, missing subtitles %d, inserted %d, updated %d",
		scanResult.ScannedVideoFiles,
		scanResult.MissingSubtitleFiles,
		inserted,
		updated,
	)
	_ = s.repo.UpdateJob(ctx, jobID, "completed", details, "")
	s.events.Publish("job.updated", map[string]any{
		"id":                  jobID,
		"status":              "completed",
		"scanned_video":       scanResult.ScannedVideoFiles,
		"missing_subtitles":   scanResult.MissingSubtitleFiles,
		"inserted_or_updated": inserted + updated,
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	stream := s.events.Subscribe()
	defer s.events.Unsubscribe(stream)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-stream:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	missingOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("missing_sub")), "true")
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	items, err := s.repo.ListMedia(r.Context(), missingOnly, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) staticHandler() http.Handler {
	index := filepath.Join(s.cfg.StaticDir, "index.html")
	if _, err := os.Stat(index); err == nil {
		fs := http.FileServer(http.Dir(s.cfg.StaticDir))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			relativePath := strings.TrimPrefix(r.URL.Path, "/")
			path := filepath.Join(s.cfg.StaticDir, filepath.Clean(relativePath))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				fs.ServeHTTP(w, r)
				return
			}
			http.ServeFile(w, r, index)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"message": "frontend build not found. run npm run build in /web or use Docker image",
		})
	})
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func encrypt(plaintext []byte, secret string) (string, error) {
	if strings.TrimSpace(secret) == "" {
		// Bootstrap fallback; caller should set APP_SECRET for real deployments.
		return "plain:" + base64.StdEncoding.EncodeToString(plaintext), nil
	}

	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return "enc:" + base64.StdEncoding.EncodeToString(ciphertext), nil
}
