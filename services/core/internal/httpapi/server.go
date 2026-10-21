package httpapi

import (
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"marketplace/core/internal/api"
	"marketplace/core/internal/apperr"
	"marketplace/core/internal/auth"
	"marketplace/core/internal/config"
	"marketplace/core/internal/order"
	"marketplace/core/internal/product"
	"marketplace/core/internal/promo"
	"marketplace/core/internal/store"
)

type Server struct {
	auth     *auth.Service
	products *product.Service
	orders   *order.Service
	promos   *promo.Service
}

func New(cfg config.Config, st *store.Store) (http.Handler, error) {
	spec, err := api.GetSwagger()
	if err != nil {
		return nil, err
	}
	validator, err := newValidator(spec)
	if err != nil {
		return nil, err
	}
	authService := auth.NewService(st, cfg.JWTSecret, cfg.AccessTTL, cfg.RefreshTTL)
	server := &Server{
		auth:     authService,
		products: product.NewService(st),
		orders:   order.NewService(st, cfg.OrderRateLimit),
		promos:   promo.NewService(st),
	}

	router := chi.NewRouter()
	router.Use(accessLog(os.Stdout))
	router.Use(recoverer)
	router.Use(requestID)
	router.Use(authenticate(authService))
	router.Use(validator)
	router.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, routeNotFound())
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, methodNotAllowed())
	})
	return api.HandlerWithOptions(server, api.ChiServerOptions{
		BaseRouter: router,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, apperr.WithDetails(http.StatusBadRequest, "VALIDATION_ERROR", "ошибка валидации", apperr.Fields("request", err.Error())))
		},
	}), nil
}
