CREATE TABLE IF NOT EXISTS app_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    language_priority TEXT NOT NULL,
    auto_replace_existing INTEGER NOT NULL DEFAULT 0,
    subtitle_output_path TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS provider_credentials (
    name TEXT PRIMARY KEY,
    secret_blob TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_type TEXT NOT NULL,
    title TEXT NOT NULL,
    year INTEGER,
    season INTEGER,
    episode INTEGER,
    file_path TEXT NOT NULL UNIQUE,
    media_hash TEXT,
    has_subtitle INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS subtitle_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_item_id INTEGER NOT NULL,
    language TEXT NOT NULL,
    provider_name TEXT NOT NULL,
    release_name TEXT,
    file_path TEXT NOT NULL,
    checksum TEXT,
    created_at TEXT NOT NULL,
    FOREIGN KEY (media_item_id) REFERENCES media_items(id)
);

CREATE TABLE IF NOT EXISTS subtitle_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_item_id INTEGER,
    provider_name TEXT NOT NULL,
    candidate_id TEXT NOT NULL,
    score REAL NOT NULL,
    language TEXT,
    payload_json TEXT,
    expires_at TEXT,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    details TEXT,
    error TEXT,
    retries INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_key TEXT NOT NULL UNIQUE,
    rule_value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    level TEXT NOT NULL,
    action TEXT NOT NULL,
    message TEXT NOT NULL,
    payload_json TEXT,
    created_at TEXT NOT NULL
);
