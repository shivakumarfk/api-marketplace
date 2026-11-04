package service

import (
	"context"
	"errors"

	"apimarket/internal/domain"
)

type CatalogService struct {
	Catalog   CatalogRepo
	Endpoints EndpointRepo
}

func (s *CatalogService) ListPublished(ctx context.Context) ([]CatalogEntry, error) {
	return s.Catalog.ListPublished(ctx)
}

// CatalogDetail is the public contract of one API: its plans and documented endpoints.
type CatalogDetail struct {
	CatalogEntry
	Endpoints []domain.Endpoint
}

func (s *CatalogService) GetPublished(ctx context.Context, slug string) (CatalogDetail, error) {
	e, err := s.Catalog.GetPublished(ctx, slug)
	if errors.Is(err, domain.ErrNotFound) {
		return CatalogDetail{}, domain.NotFound("API_NOT_FOUND", "no published API at path /"+slug)
	}
	if err != nil {
		return CatalogDetail{}, err
	}
	eps, err := s.Endpoints.ListByAPI(ctx, e.API.ID)
	return CatalogDetail{CatalogEntry: e, Endpoints: eps}, err
}
