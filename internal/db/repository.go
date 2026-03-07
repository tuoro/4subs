package db

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gayhub/4subs/internal/config"
	"github.com/gayhub/4subs/internal/model"
	_ "modernc.org/sqlite"
)

//go:embed migrations/001_init.sql
var initSQL string

type Repository struct {
	db *sql.DB
}

type CreateJobInput struct {
	MediaAssetID   *int64
	MediaPath      string
	FileName       string
	SourceLanguage string
	TargetLanguage string
	Provider       string
	OutputFormats  []string
	Details        string
}

func Open(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := migrate(database); err != nil {
		_ = database.Close()
		return nil, err
	}
	return database, nil
}

func migrate(database *sql.DB) error {
	_, err := database.Exec(initSQL)
	return err
}

func NewRepository(database *sql.DB) *Repository {
	return &Repository{db: database}
}

func (r *Repository) EnsureDefaults(ctx context.Context, cfg config.Config) error {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM app_settings WHERE id = 1`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	settings := model.AppSettings{
		MediaPaths:          cfg.MediaPaths,
		SourceLanguage:      "auto",
		TargetLanguage:      "zh-CN",
		BilingualLayout:     "origin_above",
		OutputFormats:       []string{"srt"},
		TranslationProvider: cfg.TranslationProvider,
		TranslationModel:    cfg.DeepSeekModel,
		TranslationPrompt:   "逐条翻译字幕文本，保留原意、语气和专有名词，不要合并条目。",
		MaxSubtitlePerBatch: 30,
		UpdatedAt:           time.Now().UTC(),
	}
	return r.SaveSettings(ctx, settings)
}

func (r *Repository) GetSettings(ctx context.Context) (model.AppSettings, error) {
	var (
		mediaPathsJSON    string
		outputFormatsJSON string
		updatedAtRaw      string
		settings          model.AppSettings
	)
	row := r.db.QueryRowContext(ctx, `
		SELECT media_paths_json, source_language, target_language, bilingual_layout,
		       output_formats_json, translation_provider, translation_model,
		       translation_prompt, max_subtitle_per_batch, updated_at
		FROM app_settings WHERE id = 1`)
	if err := row.Scan(
		&mediaPathsJSON,
		&settings.SourceLanguage,
		&settings.TargetLanguage,
		&settings.BilingualLayout,
		&outputFormatsJSON,
		&settings.TranslationProvider,
		&settings.TranslationModel,
		&settings.TranslationPrompt,
		&settings.MaxSubtitlePerBatch,
		&updatedAtRaw,
	); err != nil {
		return model.AppSettings{}, err
	}
	if err := json.Unmarshal([]byte(mediaPathsJSON), &settings.MediaPaths); err != nil {
		return model.AppSettings{}, err
	}
	if err := json.Unmarshal([]byte(outputFormatsJSON), &settings.OutputFormats); err != nil {
		return model.AppSettings{}, err
	}
	settings.UpdatedAt = parseTime(updatedAtRaw)
	return settings, nil
}

func (r *Repository) SaveSettings(ctx context.Context, settings model.AppSettings) error {
	if len(settings.MediaPaths) == 0 {
		return errors.New("至少需要一个媒体目录")
	}
	if strings.TrimSpace(settings.SourceLanguage) == "" {
		settings.SourceLanguage = "auto"
	}
	if strings.TrimSpace(settings.TargetLanguage) == "" {
		settings.TargetLanguage = "zh-CN"
	}
	if strings.TrimSpace(settings.BilingualLayout) == "" {
		settings.BilingualLayout = "origin_above"
	}
	if len(settings.OutputFormats) == 0 {
		settings.OutputFormats = []string{"srt"}
	}
	if strings.TrimSpace(settings.TranslationProvider) == "" {
		settings.TranslationProvider = "deepseek"
	}
	if strings.TrimSpace(settings.TranslationModel) == "" {
		settings.TranslationModel = "deepseek-chat"
	}
	if settings.MaxSubtitlePerBatch <= 0 {
		settings.MaxSubtitlePerBatch = 30
	}
	settings.UpdatedAt = time.Now().UTC()

	mediaPathsJSON, err := json.Marshal(settings.MediaPaths)
	if err != nil {
		return err
	}
	outputFormatsJSON, err := json.Marshal(settings.OutputFormats)
	if err != nil {
		return err
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO app_settings (
			id, media_paths_json, source_language, target_language, bilingual_layout,
			output_formats_json, translation_provider, translation_model,
			translation_prompt, max_subtitle_per_batch, updated_at
		) VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			media_paths_json = excluded.media_paths_json,
			source_language = excluded.source_language,
			target_language = excluded.target_language,
			bilingual_layout = excluded.bilingual_layout,
			output_formats_json = excluded.output_formats_json,
			translation_provider = excluded.translation_provider,
			translation_model = excluded.translation_model,
			translation_prompt = excluded.translation_prompt,
			max_subtitle_per_batch = excluded.max_subtitle_per_batch,
			updated_at = excluded.updated_at`,
		string(mediaPathsJSON),
		settings.SourceLanguage,
		settings.TargetLanguage,
		settings.BilingualLayout,
		string(outputFormatsJSON),
		settings.TranslationProvider,
		settings.TranslationModel,
		settings.TranslationPrompt,
		settings.MaxSubtitlePerBatch,
		settings.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

func (r *Repository) ReplaceMediaAssets(ctx context.Context, assets []model.MediaAsset) error {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = transaction.Rollback()
		}
	}()

	if _, err = transaction.ExecContext(ctx, `DELETE FROM media_assets`); err != nil {
		return err
	}

	statement, err := transaction.PrepareContext(ctx, `
		INSERT INTO media_assets (title, root_path, relative_path, file_path, file_size, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = statement.Close() }()

	for _, asset := range assets {
		updatedAt := asset.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = time.Now().UTC()
		}
		if _, err = statement.ExecContext(ctx,
			asset.Title,
			asset.RootPath,
			asset.RelativePath,
			asset.FilePath,
			asset.FileSize,
			asset.Status,
			updatedAt.Format(time.RFC3339),
		); err != nil {
			return err
		}
	}

	err = transaction.Commit()
	return err
}

func (r *Repository) ListMediaAssets(ctx context.Context, limit int) ([]model.MediaAsset, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, root_path, relative_path, file_path, file_size, status, updated_at
		FROM media_assets
		ORDER BY updated_at DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	assets := make([]model.MediaAsset, 0)
	for rows.Next() {
		var asset model.MediaAsset
		var updatedAtRaw string
		if err := rows.Scan(
			&asset.ID,
			&asset.Title,
			&asset.RootPath,
			&asset.RelativePath,
			&asset.FilePath,
			&asset.FileSize,
			&asset.Status,
			&updatedAtRaw,
		); err != nil {
			return nil, err
		}
		asset.UpdatedAt = parseTime(updatedAtRaw)
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (r *Repository) CreateJob(ctx context.Context, input CreateJobInput) (model.SubtitleJob, error) {
	if strings.TrimSpace(input.MediaPath) == "" {
		return model.SubtitleJob{}, errors.New("媒体路径不能为空")
	}
	if strings.TrimSpace(input.FileName) == "" {
		return model.SubtitleJob{}, errors.New("文件名不能为空")
	}
	if len(input.OutputFormats) == 0 {
		input.OutputFormats = []string{"srt"}
	}
	if strings.TrimSpace(input.Provider) == "" {
		input.Provider = "deepseek"
	}
	if strings.TrimSpace(input.SourceLanguage) == "" {
		input.SourceLanguage = "auto"
	}
	if strings.TrimSpace(input.TargetLanguage) == "" {
		input.TargetLanguage = "zh-CN"
	}

	outputFormatsJSON, err := json.Marshal(input.OutputFormats)
	if err != nil {
		return model.SubtitleJob{}, err
	}

	now := time.Now().UTC()
	job := model.SubtitleJob{
		ID:             fmt.Sprintf("job_%d", now.UnixNano()),
		MediaAssetID:   input.MediaAssetID,
		MediaPath:      input.MediaPath,
		FileName:       input.FileName,
		Status:         "queued",
		CurrentStage:   "queued",
		Progress:       0,
		SourceLanguage: input.SourceLanguage,
		TargetLanguage: input.TargetLanguage,
		Provider:       input.Provider,
		OutputFormats:  input.OutputFormats,
		Details:        input.Details,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO subtitle_jobs (
			id, media_asset_id, media_path, file_name, status, current_stage, progress,
			source_language, target_language, provider, output_formats_json, details,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		nullableInt64(job.MediaAssetID),
		job.MediaPath,
		job.FileName,
		job.Status,
		job.CurrentStage,
		job.Progress,
		job.SourceLanguage,
		job.TargetLanguage,
		job.Provider,
		string(outputFormatsJSON),
		job.Details,
		job.CreatedAt.Format(time.RFC3339),
		job.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return model.SubtitleJob{}, err
	}
	return job, nil
}

func (r *Repository) ListJobs(ctx context.Context, limit int) ([]model.SubtitleJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, media_asset_id, media_path, file_name, status, current_stage, progress,
		       source_language, target_language, provider, output_formats_json, details,
		       created_at, updated_at
		FROM subtitle_jobs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	jobs := make([]model.SubtitleJob, 0)
	for rows.Next() {
		var (
			job               model.SubtitleJob
			outputFormatsJSON string
			mediaAssetID      sql.NullInt64
			createdAtRaw      string
			updatedAtRaw      string
		)
		if err := rows.Scan(
			&job.ID,
			&mediaAssetID,
			&job.MediaPath,
			&job.FileName,
			&job.Status,
			&job.CurrentStage,
			&job.Progress,
			&job.SourceLanguage,
			&job.TargetLanguage,
			&job.Provider,
			&outputFormatsJSON,
			&job.Details,
			&createdAtRaw,
			&updatedAtRaw,
		); err != nil {
			return nil, err
		}
		if mediaAssetID.Valid {
			job.MediaAssetID = &mediaAssetID.Int64
		}
		if err := json.Unmarshal([]byte(outputFormatsJSON), &job.OutputFormats); err != nil {
			return nil, err
		}
		job.CreatedAt = parseTime(createdAtRaw)
		job.UpdatedAt = parseTime(updatedAtRaw)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *Repository) CountMediaAssets(ctx context.Context) (int, error) {
	return r.countByQuery(ctx, `SELECT COUNT(*) FROM media_assets`)
}

func (r *Repository) CountPendingJobs(ctx context.Context) (int, error) {
	return r.countByQuery(ctx, `SELECT COUNT(*) FROM subtitle_jobs WHERE status IN ('queued', 'running')`)
}

func (r *Repository) countByQuery(ctx context.Context, query string) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func parseTime(raw string) time.Time {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

