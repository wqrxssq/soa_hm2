package httpapi

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"marketplace/core/internal/api"
	"marketplace/core/internal/order"
	"marketplace/core/internal/store"
)

func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: "ok"})
}

func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	var body api.RegisterRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	user, err := s.auth.Register(r.Context(), string(body.Email), body.Password, string(body.Role))
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := parseUUID(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.UserResponse{
		Id:    id,
		Email: openapi_types.Email(user.Email),
		Role:  api.UserRole(user.Role),
	})
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body api.LoginRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	pair, err := s.auth.Login(r.Context(), string(body.Email), body.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
	})
}

func (s *Server) Refresh(w http.ResponseWriter, r *http.Request) {
	var body api.RefreshRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	pair, err := s.auth.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.TokenPair{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresIn:    pair.ExpiresIn,
	})
}

func (s *Server) ListProducts(w http.ResponseWriter, r *http.Request, params api.ListProductsParams) {
	page := 0
	if params.Page != nil {
		page = *params.Page
	}
	size := 20
	if params.Size != nil {
		size = *params.Size
	}
	var status *string
	if params.Status != nil {
		value := string(*params.Status)
		status = &value
	}
	products, total, err := s.products.List(r.Context(), status, params.Category, page, size)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]api.ProductResponse, 0, len(products))
	for _, product := range products {
		item, err := toProduct(product)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, api.ProductListResponse{
		Items:         items,
		TotalElements: total,
		Page:          page,
		Size:          size,
	})
}

func (s *Server) CreateProduct(w http.ResponseWriter, r *http.Request) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body api.ProductCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	created, err := s.products.Create(r.Context(), actor, store.ProductWrite{
		Name:        body.Name,
		Description: body.Description,
		Price:       moneyDecimal(body.Price),
		Stock:       body.Stock,
		Category:    body.Category,
		Status:      string(body.Status),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toProduct(created)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) GetProduct(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	product, err := s.products.Get(r.Context(), id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toProduct(product)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) UpdateProduct(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body api.ProductUpdate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	updated, err := s.products.Update(r.Context(), actor, id.String(), store.ProductWrite{
		Name:        body.Name,
		Description: body.Description,
		Price:       moneyDecimal(body.Price),
		Stock:       body.Stock,
		Category:    body.Category,
		Status:      string(body.Status),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toProduct(updated)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) DeleteProduct(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	archived, err := s.products.Archive(r.Context(), actor, id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toProduct(archived)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body api.OrderCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	created, err := s.orders.Create(r.Context(), actor, toItemInputs(body.Items), body.PromoCode)
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toOrder(created)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) GetOrder(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	found, err := s.orders.Get(r.Context(), actor, id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toOrder(found)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) UpdateOrder(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body api.OrderUpdate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	updated, err := s.orders.Update(r.Context(), actor, id.String(), toItemInputs(body.Items))
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toOrder(updated)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	canceled, err := s.orders.Cancel(r.Context(), actor, id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toOrder(canceled)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) AdvanceOrder(w http.ResponseWriter, r *http.Request, id api.ResourceId) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	advanced, err := s.orders.Advance(r.Context(), actor, id.String())
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toOrder(advanced)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) CreatePromoCode(w http.ResponseWriter, r *http.Request) {
	actor, err := requireActor(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body api.PromoCodeCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	created, err := s.promos.Create(r.Context(), actor, store.PromoWrite{
		Code:           body.Code,
		DiscountType:   string(body.DiscountType),
		DiscountValue:  moneyDecimal(body.DiscountValue),
		MinOrderAmount: moneyDecimal(body.MinOrderAmount),
		MaxUses:        body.MaxUses,
		ValidFrom:      body.ValidFrom,
		ValidUntil:     body.ValidUntil,
		Active:         active,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	response, err := toPromo(created)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func toItemInputs(items []api.OrderItemInput) []order.ItemInput {
	result := make([]order.ItemInput, 0, len(items))
	for _, item := range items {
		result = append(result, order.ItemInput{
			ProductID: item.ProductId.String(),
			Quantity:  item.Quantity,
		})
	}
	return result
}
