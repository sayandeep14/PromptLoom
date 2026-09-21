package store

import (
	"context"

	"github.com/sayandeep14/PromptLoom/server/internal/models"
)

// PG is the PostgreSQL-backed store. It uses the shared pool in package db.
type PG struct{}

func (PG) ListVaults(ctx context.Context) ([]models.ListItem, error) { return ListVaults(ctx) }
func (PG) GetVault(ctx context.Context, slug string) (*models.Vault, error) {
	return GetVault(ctx, slug)
}
func (PG) GetBundle(ctx context.Context, slug string) (*models.Bundle, error) {
	return GetBundle(ctx, slug)
}
func (PG) UpsertVault(ctx context.Context, b *models.Bundle, who models.Identity) error {
	return UpsertVault(ctx, b, who)
}
func (PG) DeleteVault(ctx context.Context, slug string, who models.Identity) (bool, error) {
	return DeleteVault(ctx, slug, who)
}
