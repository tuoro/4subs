package db

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/model"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Repository struct {
	db *sql.DB
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func applyMigrations(db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sqlBytes, readErr := migrationFS.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if _, execErr := db.Exec(string(sqlBytes)); execErr != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), execErr)
		}
	}
	return nil
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EnsureDefaults(ctx context.Context, cfg config.Config) error {
	now := time.Now().UTC().Format(time.RFC3339)
	lang, _ := json.Marshal([]string{"bilingual", "zh-cn", "zh-tw"})

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO app_settings (id, language_priority, auto_replace_existing, subtitle_output_path, updated_at)
		VALUES (1, ?, 0, ?, ?)
		ON CONFLICT(id) DO NOTHING;
	`, string(lang), cfg.SubtitleOutputPath, now)
	if err != nil {
		return err
	}

	providers := []struct {
		Name string
		Blob string
	}{
		{Name: "assrt", Blob: ""},
		{Name: "opensubtitles", Blob: ""},
	}
	for _, p := range providers {
		if _, err := r.db.ExecContext(ctx, `
			INSERT INTO provider_credentials (name, secret_blob, updated_at)
			VALUES (?, ?, ?)
			ON CONFLICT(name) DO NOTHING;
		`, p.Name, p.Blob, now); err != nil {
			return err
		}
	}

	if cfg.ASSRTToken != "" {
		if err := r.SaveProviderCredential(ctx, "assrt", cfg.ASSRTToken); err != nil {
			return err
		}
	}

	if cfg.OpenSubtitlesAPIKey != "" {
		seed := map[string]string{
			"api_key":    cfg.OpenSubtitlesAPIKey,
			"username":   cfg.OpenSubtitlesUser,
			"password":   cfg.OpenSubtitlesPass,
			"user_agent": cfg.OpenSubtitlesUA,
		}
		if err := r.SaveProviderCredentialJSON(ctx, "opensubtitles", seed); err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) GetSettings(ctx context.Context) (model.Settings, error) {
	var out model.Settings
	var rawPriority string
	var autoReplace int
	row := r.db.QueryRowContext(ctx, `
		SELECT language_priority, auto_replace_existing, subtitle_output_path
		FROM app_settings
		WHERE id = 1;
	`)
	if err := row.Scan(&rawPriority, &autoReplace, &out.SubtitleOutputPath); err != nil {
		return out, err
	}
	if err := json.Unmarshal([]byte(rawPriority), &out.LanguagePriority); err != nil {
		return out, err
	}
	out.AutoReplaceExisting = autoReplace == 1
	return out, nil
}

func (r *Repository) UpdateSettings(ctx context.Context, settings model.Settings) error {
	if len(settings.LanguagePriority) == 0 {
		return errors.New("language_priority cannot be empty")
	}
	rawPriority, err := json.Marshal(settings.LanguagePriority)
	if err != nil {
		return err
	}
	autoReplace := 0
	if settings.AutoReplaceExisting {
		autoReplace = 1
	}

	_, err = r.db.ExecContext(ctx, `
		UPDATE app_settings
		SET language_priority = ?, auto_replace_existing = ?, subtitle_output_path = ?, updated_at = ?
		WHERE id = 1;
	`, string(rawPriority), autoReplace, settings.SubtitleOutputPath, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (r *Repository) ListProviders(ctx context.Context) ([]model.ProviderStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, secret_blob
		FROM provider_credentials
		ORDER BY name;
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make([]model.ProviderStatus, 0, 2)
	for rows.Next() {
		var name string
		var blob string
		if err := rows.Scan(&name, &blob); err != nil {
			return nil, err
		}
		status := model.ProviderStatus{
			Name:           name,
			DisplayName:    displayName(name),
			Configured:     strings.TrimSpace(blob) != "",
			Enabled:        true,
			SupportsSearch: true,
			SupportsDL:     true,
		}
		if name == "assrt" {
			status.Note = "ASSRT free tier starts at 20 req/min per token+IP"
		}
		if name == "opensubtitles" {
			status.Note = "OpenSubtitles.com API only"
		}
		providers = append(providers, status)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return providers, nil
}

func displayName(name string) string {
	switch name {
	case "assrt":
		return "ASSRT"
	case "opensubtitles":
		return "OpenSubtitles.com"
	default:
		return strings.ToUpper(name)
	}
}

func (r *Repository) SaveProviderCredential(ctx context.Context, name, blob string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE provider_credentials
		SET secret_blob = ?, updated_at = ?
		WHERE name = ?;
	`, blob, time.Now().UTC().Format(time.RFC3339), name)
	return err
}

func (r *Repository) SaveProviderCredentialJSON(ctx context.Context, name string, payload map[string]string) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.SaveProviderCredential(ctx, name, string(raw))
}

func (r *Repository) CreateJob(ctx context.Context, jobType string, details string) (model.Job, error) {
	now := time.Now().UTC()
	job := model.Job{
		ID:        uuid.NewString(),
		Type:      jobType,
		Status:    "queued",
		Details:   details,
		Retries:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, details, error, retries, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, ?, ?);
	`, job.ID, job.Type, job.Status, job.Details, job.Retries, now.Format(time.RFC3339), now.Format(time.RFC3339))
	if err != nil {
		return model.Job{}, err
	}
	return job, nil
}

func (r *Repository) UpdateJobStatus(ctx context.Context, jobID string, status string, errText string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, error = ?, updated_at = ?
		WHERE id = ?;
	`, status, errText, time.Now().UTC().Format(time.RFC3339), jobID)
	return err
}

func (r *Repository) ListJobs(ctx context.Context, limit int) ([]model.Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, type, status, details, error, retries, created_at, updated_at
		FROM jobs
		ORDER BY created_at DESC
		LIMIT ?;
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]model.Job, 0, limit)
	for rows.Next() {
		var job model.Job
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&job.ID,
			&job.Type,
			&job.Status,
			&job.Details,
			&job.Error,
			&job.Retries,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}
