package promo

import (
	"context"
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

func (s *Service) Create(ctx context.Context, actor identity.Actor, in store.PromoWrite) (store.Promo, error) {
	if actor.IsUser() {
		return store.Promo{}, apperr.New(http.StatusForbidden, "ACCESS_DENIED", "недостаточно прав")
	}
	if in.ValidUntil.Before(in.ValidFrom) {
		return store.Promo{}, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields("valid_until", "must be greater than or equal to valid_from"))
	}
	created, err := store.InsertPromo(ctx, s.store.Query(), in)
	if field, ok := store.IsUnique(err); ok {
		return store.Promo{}, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields(field, "already exists"))
	}
	return created, err
}
