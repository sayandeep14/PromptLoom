-- PromptLoom Registry schema
-- Apply schema.sql for a fresh database.
-- Apply migration.sql for an existing database (adds Pack v2 columns).

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS vaults (
    id                UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    pack_id           TEXT    UNIQUE,                   -- user-managed UUID from .metadata.loom
    name              TEXT    UNIQUE NOT NULL,
    slug              TEXT    UNIQUE NOT NULL,
    description       TEXT    NOT NULL DEFAULT '',
    author            TEXT    NOT NULL DEFAULT '',
    version           TEXT    NOT NULL DEFAULT '1.0.0',
    tags              TEXT[]  NOT NULL DEFAULT '{}',
    related_libraries JSONB   NOT NULL DEFAULT '[]',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS vault_files (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vault_id    UUID NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    file_type   TEXT NOT NULL CHECK (file_type IN ('prompt', 'block', 'overlay', 'meta')),
    content     TEXT NOT NULL,
    UNIQUE (vault_id, path)
);

CREATE INDEX IF NOT EXISTS vault_files_vault_id_idx ON vault_files (vault_id);

-- Automatically bump updated_at on vault row changes.
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS vaults_updated_at ON vaults;
CREATE TRIGGER vaults_updated_at
    BEFORE UPDATE ON vaults
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
