-- Pack v2 migration — run against an existing registry database.
-- Safe to run multiple times (uses IF NOT EXISTS / DO blocks).

-- Add pack_id column to vaults (nullable for backward compat with existing rows).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name='vaults' AND column_name='pack_id'
    ) THEN
        ALTER TABLE vaults ADD COLUMN pack_id TEXT;
        CREATE UNIQUE INDEX IF NOT EXISTS vaults_pack_id_idx ON vaults (pack_id)
            WHERE pack_id IS NOT NULL;
    END IF;
END$$;

-- Add related_libraries JSONB column.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name='vaults' AND column_name='related_libraries'
    ) THEN
        ALTER TABLE vaults ADD COLUMN related_libraries JSONB NOT NULL DEFAULT '[]';
    END IF;
END$$;

-- Expand file_type check to include 'meta'.
-- PostgreSQL doesn't support ALTER CONSTRAINT directly; drop and recreate.
ALTER TABLE vault_files DROP CONSTRAINT IF EXISTS vault_files_file_type_check;
ALTER TABLE vault_files ADD CONSTRAINT vault_files_file_type_check
    CHECK (file_type IN ('prompt', 'block', 'overlay', 'meta'));
