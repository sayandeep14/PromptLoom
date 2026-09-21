package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/sayandeep14/PromptLoom/server/internal/db"
	"github.com/sayandeep14/PromptLoom/server/internal/models"
)

var jsonMarshal = json.Marshal

// ListVaults returns all vaults ordered by name.
func ListVaults(ctx context.Context) ([]models.ListItem, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT v.pack_id, v.name, v.slug, v.description, v.author, v.owner, v.version,
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
			&it.Author, &it.Owner, &it.Version, &it.Tags, &it.UpdatedAt, &it.FileCount); err != nil {
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
		SELECT v.id, v.pack_id, v.name, v.slug, v.description, v.author, v.owner, v.version,
		       v.tags, v.related_libraries, v.created_at, v.updated_at,
		       COUNT(f.id) AS file_count
		FROM vaults v
		LEFT JOIN vault_files f ON f.vault_id = v.id
		WHERE v.slug = $1
		GROUP BY v.id`, slug).Scan(
		&v.ID, &packID, &v.Name, &v.Slug, &v.Description, &v.Author, &v.Owner, &v.Version,
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
		ORDER BY path COLLATE "C"`, vault.ID)
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

// DeleteVault removes a vault and all its files by slug, if who owns it or is an admin. The
// ownership test is part of the DELETE itself, so there is no window between checking and
// deleting. deleted is false when no such pack exists; models.ErrNotOwner when it belongs to
// someone else.
func DeleteVault(ctx context.Context, slug string, who models.Identity) (bool, error) {
	tag, err := db.Pool.Exec(ctx, `DELETE FROM vaults WHERE slug = $1 AND (owner = $2 OR $3)`, slug, who.Name, who.Admin)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	var exists bool
	if err := db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM vaults WHERE slug = $1)`, slug).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, models.ErrNotOwner
	}
	return false, nil
}

// UpsertVault creates or replaces a vault and its files inside a transaction. A pack is owned by
// whoever first published it; only that publisher (or an admin) may replace it afterwards. Packs
// that predate ownership have an empty owner and can be changed by admins only. The rule is part
// of the ON CONFLICT clause, so it cannot race with another publisher.
func UpsertVault(ctx context.Context, bundle *models.Bundle, who models.Identity) error {
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

	// tags is NOT NULL: a nil slice would be sent as SQL NULL and fail the insert.
	tags := bundle.Tags
	if tags == nil {
		tags = []string{}
	}

	var vaultID string
	err = tx.QueryRow(ctx, `
		INSERT INTO vaults (pack_id, name, slug, description, author, version, tags, related_libraries, owner)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (slug) DO UPDATE
		  SET pack_id           = EXCLUDED.pack_id,
		      name              = EXCLUDED.name,
		      description       = EXCLUDED.description,
		      author            = EXCLUDED.author,
		      version           = EXCLUDED.version,
		      tags              = EXCLUDED.tags,
		      related_libraries = EXCLUDED.related_libraries
		  WHERE vaults.owner = $9 OR $10
		RETURNING id`,
		nullableString(bundle.PackID), bundle.Name, bundle.Slug, bundle.Description,
		bundle.Author, bundle.Version, tags, relLibsJSON, who.Name, who.Admin,
	).Scan(&vaultID)
	if errors.Is(err, pgx.ErrNoRows) {
		// the row exists but the WHERE refused the update: someone else owns it
		return models.ErrNotOwner
	}
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
