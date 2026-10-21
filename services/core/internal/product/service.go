package product

import (
	"context"
	"errors"
	"net/http"

	"marketplace/core/internal/apperr"
	"marketplace/core/internal/identity"
	"marketplace/core/internal/store"
)

type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) Create(ctx context.Context, actor identity.Actor, in store.ProductWrite) (store.Product, error) {
	if actor.IsUser() {
		return store.Product{}, denied()
	}
	in.SellerID = actor.ID
	return store.InsertProduct(ctx, s.store.Query(), in)
}

func (s *Service) Update(ctx context.Context, actor identity.Actor, id string, in store.ProductWrite) (store.Product, error) {
	if err := s.ensureCanWrite(ctx, actor, id); err != nil {
		return store.Product{}, err
	}
	product, err := store.UpdateProduct(ctx, s.store.Query(), id, in)
	if errors.Is(err, store.ErrNotFound) {
		return store.Product{}, notFound()
	}
	return product, err
}

func (s *Service) Archive(ctx context.Context, actor identity.Actor, id string) (store.Product, error) {
	if err := s.ensureCanWrite(ctx, actor, id); err != nil {
		return store.Product{}, err
	}
	product, err := store.ArchiveProduct(ctx, s.store.Query(), id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Product{}, notFound()
	}
	return product, err
}

func (s *Service) Get(ctx context.Context, id string) (store.Product, error) {
	product, err := store.GetProduct(ctx, s.store.Query(), id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Product{}, notFound()
	}
	return product, err
}

func (s *Service) List(ctx context.Context, status, category *string, page, size int) ([]store.Product, int, error) {
	if page < 0 {
		page = 0
	}
	if size <= 0 {
		size = 20
	}
	return store.ListProducts(ctx, s.store.Query(), status, category, size, page*size)
}

func (s *Service) ensureCanWrite(ctx context.Context, actor identity.Actor, id string) error {
	if actor.IsUser() {
		return denied()
	}
	product, err := store.GetProduct(ctx, s.store.Query(), id)
	if errors.Is(err, store.ErrNotFound) {
		return notFound()
	}
	if err != nil {
		return err
	}
	if actor.IsSeller() && product.SellerID != actor.ID {
		return denied()
	}
	return nil
}

func denied() error {
	return apperr.New(http.StatusForbidden, "ACCESS_DENIED", "недостаточно прав")
}

func notFound() error {
	return apperr.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "товар не найден")
}
