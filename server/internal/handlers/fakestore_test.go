package handlers

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/sayandeepgiri/promptloom/server/internal/models"
)

// fakeStore is an in-memory Store with the same observable semantics as the
// Postgres one: upsert replaces, deleting a missing pack is not an error.
type fakeStore struct {
	mu     sync.Mutex
	packs  map[string]*models.Bundle
	err    error // when set, every call fails with it
	upsert int   // number of UpsertVault calls
}

func newFake() *fakeStore { return &fakeStore{packs: map[string]*models.Bundle{}} }

func (f *fakeStore) ListVaults(context.Context) ([]models.ListItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	var out []models.ListItem
	for _, b := range f.packs {
		out = append(out, models.ListItem{Name: b.Name, Slug: b.Slug, Version: b.Version, FileCount: len(b.Files)})
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
	return &models.Vault{Name: b.Name, Slug: b.Slug, Version: b.Version, FileCount: len(b.Files), CreatedAt: time.Now()}, nil
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

func (f *fakeStore) UpsertVault(_ context.Context, b *models.Bundle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsert++
	if f.err != nil {
		return f.err
	}
	cp := *b
	f.packs[b.Slug] = &cp
	return nil
}

func (f *fakeStore) DeleteVault(_ context.Context, slug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	delete(f.packs, slug)
	return nil
}

var errBoom = errors.New(`pq: password authentication failed for user "registry" at 10.1.2.3:5432`)
