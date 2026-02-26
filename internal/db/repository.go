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
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`); err != nil {
		return err
	}

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

		var exists int
		checkErr := db.QueryRow(`SELECT 1 FROM schema_migrations WHERE name = ? LIMIT 1;`, entry.Name()).Scan(&exists)
		if checkErr == nil {
			continue
		}
		if checkErr != nil && checkErr != sql.ErrNoRows {
			return checkErr
		}

		sqlBytes, readErr := migrationFS.ReadFile("migrations/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if _, execErr := db.Exec(string(sqlBytes)); execErr != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), execErr)
		}
		if _, insertErr := db.Exec(
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?);`,
			entry.Name(),
			time.Now().UTC().Format(time.RFC3339),
		); insertErr != nil {
			return fmt.Errorf("record migration %s: %w", entry.Name(), insertErr)
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

func (r *Repository) GetProviderCredentialBlob(ctx context.Context, name string) (string, error) {
	var blob string
	err := r.db.QueryRowContext(ctx, `
		SELECT secret_blob
		FROM provider_credentials
		WHERE name = ?
		LIMIT 1;
	`, name).Scan(&blob)
	if err != nil {
		return "", err
	}
	return blob, nil
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
	return r.UpdateJob(ctx, jobID, status, "", errText)
}

func (r *Repository) UpdateJob(ctx context.Context, jobID string, status string, details string, errText string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE jobs
		SET status = ?, details = CASE WHEN ? = '' THEN details ELSE ? END, error = ?, updated_at = ?
		WHERE id = ?;
	`, status, details, details, errText, time.Now().UTC().Format(time.RFC3339), jobID)
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

func (r *Repository) UpsertMediaItems(ctx context.Context, items []model.MediaItem) (inserted int64, updated int64, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		existedBefore, checkErr := mediaExistsByPathTx(ctx, tx, item.FilePath)
		if checkErr != nil {
			err = checkErr
			return 0, 0, err
		}

		hasSubtitle := 0
		if item.HasSubtitle {
			hasSubtitle = 1
		}

		res, execErr := tx.ExecContext(ctx, `
			INSERT INTO media_items (
				media_type, title, year, season, episode, file_path, media_hash, has_subtitle, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(file_path) DO UPDATE SET
				media_type = excluded.media_type,
				title = excluded.title,
				year = excluded.year,
				season = excluded.season,
				episode = excluded.episode,
				media_hash = excluded.media_hash,
				has_subtitle = excluded.has_subtitle,
				updated_at = excluded.updated_at;
		`,
			item.MediaType,
			item.Title,
			nullableInt(item.Year),
			nullableInt(item.Season),
			nullableInt(item.Episode),
			item.FilePath,
			item.MediaHash,
			hasSubtitle,
			now,
			now,
		)
		if execErr != nil {
			err = execErr
			return 0, 0, err
		}
		rows, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			continue
		}
		if existedBefore {
			updated += rows
		} else {
			inserted += rows
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, updated, nil
}

func mediaExistsByPathTx(ctx context.Context, tx *sql.Tx, filePath string) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM media_items
		WHERE file_path = ?
		LIMIT 1;
	`, filePath).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) ListMedia(ctx context.Context, missingOnly bool, limit int) ([]model.MediaItem, error) {
	if limit <= 0 {
		limit = 200
	}

	query := `
		SELECT id, media_type, title, year, season, episode, file_path, media_hash, has_subtitle, created_at, updated_at
		FROM media_items
	`
	args := make([]any, 0, 2)
	if missingOnly {
		query += " WHERE has_subtitle = 0 "
	}
	query += " ORDER BY updated_at DESC LIMIT ?;"
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.MediaItem, 0, limit)
	for rows.Next() {
		var item model.MediaItem
		var year sql.NullInt64
		var season sql.NullInt64
		var episode sql.NullInt64
		var hasSubtitle int
		var createdAt string
		var updatedAt string
		if err := rows.Scan(
			&item.ID,
			&item.MediaType,
			&item.Title,
			&year,
			&season,
			&episode,
			&item.FilePath,
			&item.MediaHash,
			&hasSubtitle,
			&createdAt,
			&updatedAt,
		); err != nil {
			return nil, err
		}
		item.Year = nullableIntFromDB(year)
		item.Season = nullableIntFromDB(season)
		item.Episode = nullableIntFromDB(episode)
		item.HasSubtitle = hasSubtitle == 1
		if parsed, err := time.Parse(time.RFC3339, createdAt); err == nil {
			item.CreatedAt = &parsed
		}
		if parsed, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			item.UpdatedAt = &parsed
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) GetMediaByID(ctx context.Context, mediaID int64) (model.MediaItem, error) {
	var item model.MediaItem
	var year sql.NullInt64
	var season sql.NullInt64
	var episode sql.NullInt64
	var hasSubtitle int
	var createdAt string
	var updatedAt string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, media_type, title, year, season, episode, file_path, media_hash, has_subtitle, created_at, updated_at
		FROM media_items
		WHERE id = ?
		LIMIT 1;
	`, mediaID).Scan(
		&item.ID,
		&item.MediaType,
		&item.Title,
		&year,
		&season,
		&episode,
		&item.FilePath,
		&item.MediaHash,
		&hasSubtitle,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.MediaItem{}, err
	}
	item.Year = nullableIntFromDB(year)
	item.Season = nullableIntFromDB(season)
	item.Episode = nullableIntFromDB(episode)
	item.HasSubtitle = hasSubtitle == 1
	if parsed, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
		item.CreatedAt = &parsed
	}
	if parsed, parseErr := time.Parse(time.RFC3339, updatedAt); parseErr == nil {
		item.UpdatedAt = &parsed
	}
	return item, nil
}

func (r *Repository) ReplaceSubtitleCandidates(ctx context.Context, mediaID int64, candidates []model.SubtitleCandidate) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `DELETE FROM subtitle_candidates WHERE media_item_id = ?;`, mediaID); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, candidate := range candidates {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO subtitle_candidates (
				media_item_id, provider_name, candidate_id, score, language, payload_json, expires_at, created_at,
				title, release_name, language_text, download_url, details
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
		`,
			mediaID,
			candidate.ProviderName,
			candidate.CandidateID,
			candidate.Score,
			candidate.Language,
			candidate.RawPayload,
			nullableTime(candidate.ExpiresAt),
			now,
			candidate.Title,
			candidate.ReleaseName,
			candidate.LanguageText,
			candidate.DownloadURL,
			candidate.Details,
		); err != nil {
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *Repository) ListSubtitleCandidates(ctx context.Context, mediaID int64, limit int) ([]model.SubtitleCandidate, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, media_item_id, provider_name, candidate_id, score, language, payload_json, expires_at, created_at,
		       title, release_name, language_text, download_url, details
		FROM subtitle_candidates
		WHERE media_item_id = ?
		ORDER BY score DESC, created_at DESC
		LIMIT ?;
	`, mediaID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]model.SubtitleCandidate, 0, limit)
	for rows.Next() {
		var candidate model.SubtitleCandidate
		var expiresAt sql.NullString
		var createdAt string
		if err := rows.Scan(
			&candidate.ID,
			&candidate.MediaItemID,
			&candidate.ProviderName,
			&candidate.CandidateID,
			&candidate.Score,
			&candidate.Language,
			&candidate.RawPayload,
			&expiresAt,
			&createdAt,
			&candidate.Title,
			&candidate.ReleaseName,
			&candidate.LanguageText,
			&candidate.DownloadURL,
			&candidate.Details,
		); err != nil {
			return nil, err
		}
		if expiresAt.Valid && strings.TrimSpace(expiresAt.String) != "" {
			if parsed, parseErr := time.Parse(time.RFC3339, expiresAt.String); parseErr == nil {
				candidate.ExpiresAt = &parsed
			}
		}
		if parsed, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
			candidate.CreatedAt = &parsed
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *Repository) GetSubtitleCandidateByID(ctx context.Context, candidateID int64) (model.SubtitleCandidate, error) {
	var candidate model.SubtitleCandidate
	var expiresAt sql.NullString
	var createdAt string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, media_item_id, provider_name, candidate_id, score, language, payload_json, expires_at, created_at,
		       title, release_name, language_text, download_url, details
		FROM subtitle_candidates
		WHERE id = ?
		LIMIT 1;
	`, candidateID).Scan(
		&candidate.ID,
		&candidate.MediaItemID,
		&candidate.ProviderName,
		&candidate.CandidateID,
		&candidate.Score,
		&candidate.Language,
		&candidate.RawPayload,
		&expiresAt,
		&createdAt,
		&candidate.Title,
		&candidate.ReleaseName,
		&candidate.LanguageText,
		&candidate.DownloadURL,
		&candidate.Details,
	)
	if err != nil {
		return model.SubtitleCandidate{}, err
	}
	if expiresAt.Valid && strings.TrimSpace(expiresAt.String) != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, expiresAt.String); parseErr == nil {
			candidate.ExpiresAt = &parsed
		}
	}
	if parsed, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
		candidate.CreatedAt = &parsed
	}
	return candidate, nil
}

func (r *Repository) SaveSubtitleFile(ctx context.Context, mediaID int64, language string, providerName string, releaseName string, filePath string, checksum string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO subtitle_files (media_item_id, language, provider_name, release_name, file_path, checksum, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?);
	`, mediaID, language, providerName, releaseName, filePath, checksum, now); err != nil {
		return err
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE media_items
		SET has_subtitle = 1, updated_at = ?
		WHERE id = ?;
	`, now, mediaID); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntFromDB(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	intVal := int(value.Int64)
	return &intVal
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
