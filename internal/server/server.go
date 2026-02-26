package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/db"
	"github.com/gayhub/4subs/internal/model"
	"github.com/gayhub/4subs/internal/provider"
	"github.com/gayhub/4subs/internal/provider/assrt"
	"github.com/gayhub/4subs/internal/provider/opensubtitles"
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
		api.Post("/media/{id}/search-subtitles", s.handleSearchSubtitles)
		api.Get("/media/{id}/candidates", s.handleMediaCandidates)
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

func (s *Server) handleSearchSubtitles(w http.ResponseWriter, r *http.Request) {
	mediaID, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || mediaID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid media id"))
		return
	}

	mediaItem, err := s.repo.GetMediaByID(r.Context(), mediaID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, errors.New("media not found"))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	settings, err := s.repo.GetSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	input := provider.SearchInput{
		MediaID:   mediaItem.ID,
		Title:     mediaItem.Title,
		MediaType: mediaItem.MediaType,
		Year:      mediaItem.Year,
		Season:    mediaItem.Season,
		Episode:   mediaItem.Episode,
		FilePath:  mediaItem.FilePath,
		Limit:     20,
	}

	type providerResult struct {
		Name       string
		Candidates []model.SubtitleCandidate
		Err        error
	}
	results := make([]providerResult, 0, 2)
	resultCh := make(chan providerResult, 2)

	clients := []provider.SearchProvider{
		assrt.New(settings.LanguagePriority),
		opensubtitles.New(settings.LanguagePriority),
	}

	var wg sync.WaitGroup
	for _, client := range clients {
		blob, err := s.repo.GetProviderCredentialBlob(r.Context(), client.Name())
		if err != nil {
			resultCh <- providerResult{Name: client.Name(), Err: err}
			continue
		}

		credential, parseErr := parseCredentialBlob(blob, s.cfg.AppSecret, client.Name())
		if parseErr != nil {
			resultCh <- providerResult{Name: client.Name(), Err: parseErr}
			continue
		}
		if len(credential) == 0 {
			continue
		}

		wg.Add(1)
		go func(searchClient provider.SearchProvider, cred map[string]string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()

			candidates, runErr := searchClient.Search(ctx, cred, input)
			resultCh <- providerResult{
				Name:       searchClient.Name(),
				Candidates: candidates,
				Err:        runErr,
			}
		}(client, credential)
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	allCandidates := make([]model.SubtitleCandidate, 0, 64)
	errorsByProvider := make(map[string]string)
	for result := range resultCh {
		results = append(results, result)
		if result.Err != nil {
			errorsByProvider[result.Name] = result.Err.Error()
			continue
		}
		allCandidates = append(allCandidates, result.Candidates...)
	}

	sort.Slice(allCandidates, func(i, j int) bool {
		return allCandidates[i].Score > allCandidates[j].Score
	})

	if err := s.repo.ReplaceSubtitleCandidates(r.Context(), mediaID, allCandidates); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	s.events.Publish("candidates.updated", map[string]any{
		"media_id":    mediaID,
		"count":       len(allCandidates),
		"providerErr": errorsByProvider,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"media_id":    mediaID,
		"count":       len(allCandidates),
		"candidates":  allCandidates,
		"errors":      errorsByProvider,
		"providerRun": len(results),
	})
}

func (s *Server) handleMediaCandidates(w http.ResponseWriter, r *http.Request) {
	mediaID, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || mediaID <= 0 {
		writeError(w, http.StatusBadRequest, errors.New("invalid media id"))
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, parseErr := strconv.Atoi(raw); parseErr == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	candidates, err := s.repo.ListSubtitleCandidates(r.Context(), mediaID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, candidates)
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

func decrypt(ciphertext string, secret string) ([]byte, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("app secret is empty")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce := raw[:gcm.NonceSize()]
	payload := raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func parseCredentialBlob(blob string, secret string, providerName string) (map[string]string, error) {
	trimmed := strings.TrimSpace(blob)
	if trimmed == "" {
		return map[string]string{}, nil
	}

	parseJSON := func(raw []byte) (map[string]string, error) {
		out := make(map[string]string)
		if err := json.Unmarshal(raw, &out); err == nil && len(out) > 0 {
			return out, nil
		}
		return nil, errors.New("credential json invalid")
	}

	if strings.HasPrefix(trimmed, "plain:") {
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(trimmed, "plain:"))
		if err != nil {
			return nil, err
		}
		if parsed, err := parseJSON(payload); err == nil {
			return parsed, nil
		}
		if providerName == "assrt" {
			return map[string]string{"token": string(payload)}, nil
		}
		return nil, errors.New("invalid plain credential payload")
	}

	if strings.HasPrefix(trimmed, "enc:") {
		payload, err := decrypt(strings.TrimPrefix(trimmed, "enc:"), secret)
		if err != nil {
			return nil, err
		}
		if parsed, err := parseJSON(payload); err == nil {
			return parsed, nil
		}
		return nil, errors.New("invalid encrypted credential payload")
	}

	if strings.HasPrefix(trimmed, "{") {
		if parsed, err := parseJSON([]byte(trimmed)); err == nil {
			return parsed, nil
		}
	}

	// Legacy assrt token format.
	if providerName == "assrt" {
		return map[string]string{"token": trimmed}, nil
	}
	return nil, errors.New("unsupported credential format")
}
