package handlers

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/sayandeep14/PromptLoom/server/internal/models"
)

// fakeStore is an in-memory Store with the same observable semantics as the
// Postgres one: upsert replaces, deleting a missing pack is not an error.
type fakeStore struct {
	mu     sync.Mutex
	packs  map[string]*models.Bundle
	owners map[string]string
	err    error // when set, every call fails with it
	upsert int   // number of UpsertVault calls
}

func newFake() *fakeStore {
	return &fakeStore{packs: map[string]*models.Bundle{}, owners: map[string]string{}}
}

func (f *fakeStore) ListVaults(context.Context) ([]models.ListItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []models.ListItem
	for _, b := range f.packs {
		out = append(out, models.ListItem{Name: b.Name, Slug: b.Slug, Version: b.Version, FileCount: len(b.Files), Owner: f.owners[b.Slug]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeStore) GetVault(_ context.Context, slug string) (*models.Vault, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	b, ok := f.packs[slug]
	if !ok {
		return nil, nil
	}
	return &models.Vault{Name: b.Name, Slug: b.Slug, Version: b.Version, FileCount: len(b.Files), Owner: f.owners[b.Slug], CreatedAt: time.Now()}, nil
}

func (f *fakeStore) GetBundle(_ context.Context, slug string) (*models.Bundle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	b, ok := f.packs[slug]
	if !ok {
		return nil, nil
	}
	cp := *b
	return &cp, nil
}

func (f *fakeStore) UpsertVault(_ context.Context, b *models.Bundle, who models.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsert++
	if f.err != nil {
		return f.err
	}
	if _, exists := f.packs[b.Slug]; exists && !who.Admin && f.owners[b.Slug] != who.Name {
		return models.ErrNotOwner
	}
	cp := *b
	f.packs[b.Slug] = &cp
	if _, exists := f.owners[b.Slug]; !exists {
		f.owners[b.Slug] = who.Name // the first publisher owns the pack; replacing keeps the owner
	}
	return nil
}

func (f *fakeStore) DeleteVault(_ context.Context, slug string, who models.Identity) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if _, exists := f.packs[slug]; !exists {
		return false, nil
	}
	if !who.Admin && f.owners[slug] != who.Name {
		return false, models.ErrNotOwner
	}
	delete(f.packs, slug)
	delete(f.owners, slug)
	return true, nil
}

var errBoom = errors.New(`pq: password authentication failed for user "registry" at 10.1.2.3:5432`)
