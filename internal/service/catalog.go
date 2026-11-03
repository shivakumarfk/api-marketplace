package service

import "context"

type CatalogService struct{ Catalog CatalogRepo }

func (s *CatalogService) ListPublished(ctx context.Context) ([]CatalogEntry, error) {
	return s.Catalog.ListPublished(ctx)
}
