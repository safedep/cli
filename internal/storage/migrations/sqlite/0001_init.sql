CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    applied_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS kv (
    scope      TEXT NOT NULL,
    namespace  TEXT NOT NULL,
    key        TEXT NOT NULL,
    value      BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    expires_at INTEGER,
    PRIMARY KEY (scope, namespace, key)
) STRICT;

CREATE INDEX IF NOT EXISTS kv_expires
    ON kv(expires_at) WHERE expires_at IS NOT NULL;
