package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sayandeepgiri/promptloom/server/internal/db"
	"github.com/sayandeepgiri/promptloom/server/internal/models"
)

var jsonMarshal = json.Marshal

// ListVaults returns all vaults ordered by name.
func ListVaults(ctx context.Context) ([]models.ListItem, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT v.pack_id, v.name, v.slug, v.description, v.author, v.version,
		       v.tags, v.updated_at,
		       COUNT(f.id) AS file_count
		FROM vaults v
		LEFT JOIN vault_files f ON f.vault_id = v.id
		GROUP BY v.id
		ORDER BY v.name`)
	if err != nil {
		return nil, fmt.Errorf("list vaults: %w", err)
	}
	defer rows.Close()

	var items []models.ListItem
	for rows.Next() {
		var it models.ListItem
		var packID *string // nullable in legacy rows
		if err := rows.Scan(&packID, &it.Name, &it.Slug, &it.Description,
			&it.Author, &it.Version, &it.Tags, &it.UpdatedAt, &it.FileCount); err != nil {
			return nil, err
		}
		if packID != nil {
			it.PackID = *packID
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// GetVault returns metadata for a single vault by slug.
func GetVault(ctx context.Context, slug string) (*models.Vault, error) {
	var v models.Vault
	var packID *string // nullable in legacy rows
	err := db.Pool.QueryRow(ctx, `
		SELECT v.id, v.pack_id, v.name, v.slug, v.description, v.author, v.version,
		       v.tags, v.related_libraries, v.created_at, v.updated_at,
		       COUNT(f.id) AS file_count
		FROM vaults v
		LEFT JOIN vault_files f ON f.vault_id = v.id
		WHERE v.slug = $1
		GROUP BY v.id`, slug).Scan(
		&v.ID, &packID, &v.Name, &v.Slug, &v.Description, &v.Author, &v.Version,
		&v.Tags, &v.RelatedLibraries, &v.CreatedAt, &v.UpdatedAt, &v.FileCount)
	if packID != nil {
		v.PackID = *packID
	}
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get vault: %w", err)
	}
	return &v, nil
}

// GetBundle returns the full bundle (metadata + all source files) for a vault.
func GetBundle(ctx context.Context, slug string) (*models.Bundle, error) {
	vault, err := GetVault(ctx, slug)
	if err != nil {
		return nil, err
	}
	if vault == nil {
		return nil, nil
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT path, file_type, content
		FROM vault_files
		WHERE vault_id = $1
		ORDER BY path`, vault.ID)
	if err != nil {
		return nil, fmt.Errorf("get bundle files: %w", err)
	}
	defer rows.Close()

	bundle := &models.Bundle{
		PackID:           vault.PackID,
		Name:             vault.Name,
		Slug:             vault.Slug,
		Version:          vault.Version,
		Description:      vault.Description,
		Author:           vault.Author,
		Tags:             vault.Tags,
		RelatedLibraries: vault.RelatedLibraries,
	}
	for rows.Next() {
		var f models.BundleFile
		if err := rows.Scan(&f.Path, &f.FileType, &f.Content); err != nil {
			return nil, err
		}
		bundle.Files = append(bundle.Files, f)
	}
	return bundle, rows.Err()
}

// DeleteVault removes a vault and all its files by slug.
func DeleteVault(ctx context.Context, slug string) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM vaults WHERE slug = $1`, slug)
	return err
}

// UpsertVault creates or replaces a vault and its files inside a transaction.
func UpsertVault(ctx context.Context, bundle *models.Bundle) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Encode related_libraries as JSON for storage.
	relLibsJSON := "[]"
	if len(bundle.RelatedLibraries) > 0 {
		b, _ := jsonMarshal(bundle.RelatedLibraries)
		relLibsJSON = string(b)
	}

	var vaultID string
	err = tx.QueryRow(ctx, `
		INSERT INTO vaults (pack_id, name, slug, description, author, version, tags, related_libraries)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (slug) DO UPDATE
		  SET pack_id           = EXCLUDED.pack_id,
		      name              = EXCLUDED.name,
		      description       = EXCLUDED.description,
		      author            = EXCLUDED.author,
		      version           = EXCLUDED.version,
		      tags              = EXCLUDED.tags,
		      related_libraries = EXCLUDED.related_libraries
		RETURNING id`,
		nullableString(bundle.PackID), bundle.Name, bundle.Slug, bundle.Description,
		bundle.Author, bundle.Version, bundle.Tags, relLibsJSON,
	).Scan(&vaultID)
	if err != nil {
		return fmt.Errorf("upsert vault: %w", err)
	}

	// Delete existing files for a clean replace.
	if _, err := tx.Exec(ctx, `DELETE FROM vault_files WHERE vault_id = $1`, vaultID); err != nil {
		return fmt.Errorf("delete old files: %w", err)
	}

	for _, f := range bundle.Files {
		if _, err := tx.Exec(ctx, `
			INSERT INTO vault_files (vault_id, path, file_type, content)
			VALUES ($1, $2, $3, $4)`,
			vaultID, f.Path, f.FileType, f.Content); err != nil {
			return fmt.Errorf("insert file %s: %w", f.Path, err)
		}
	}

	return tx.Commit(ctx)
}

// nullableString returns nil for empty strings (maps to SQL NULL).
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
