CREATE TABLE sites (
    id         TEXT PRIMARY KEY,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    expires_at TEXT    NOT NULL,
    permanent  INTEGER NOT NULL DEFAULT 0 CHECK (permanent IN (0, 1)),
    size_bytes INTEGER NOT NULL,
    file_count INTEGER NOT NULL,
    source     TEXT    NOT NULL CHECK (source IN ('api', 'web'))
);

CREATE INDEX idx_sites_reap ON sites (permanent, expires_at);
