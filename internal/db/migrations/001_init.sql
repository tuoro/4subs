CREATE TABLE IF NOT EXISTS app_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    media_paths_json TEXT NOT NULL,
    source_language TEXT NOT NULL,
    target_language TEXT NOT NULL,
    bilingual_layout TEXT NOT NULL,
    output_formats_json TEXT NOT NULL,
    translation_provider TEXT NOT NULL,
    translation_model TEXT NOT NULL,
    translation_prompt TEXT NOT NULL,
    max_subtitle_per_batch INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_assets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    root_path TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    file_path TEXT NOT NULL UNIQUE,
    file_size INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'ready',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subtitle_jobs (
    id TEXT PRIMARY KEY,
    media_asset_id INTEGER,
    media_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    status TEXT NOT NULL,
    current_stage TEXT NOT NULL,
    progress INTEGER NOT NULL DEFAULT 0,
    source_language TEXT NOT NULL,
    target_language TEXT NOT NULL,
    provider TEXT NOT NULL,
    output_formats_json TEXT NOT NULL,
    details TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (media_asset_id) REFERENCES media_assets(id)
);

