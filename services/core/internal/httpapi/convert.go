package httpapi

import (
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"marketplace/core/internal/api"
	"marketplace/core/internal/store"
)

func moneyFloat(value decimal.Decimal) float64 {
	asFloat, _ := value.Round(2).Float64()
	return asFloat
}

func moneyDecimal(value float64) decimal.Decimal {
	return decimal.NewFromFloat(value).Round(2)
}

func parseUUID(value string) (uuid.UUID, error) {
	return uuid.Parse(value)
}

func toProduct(product store.Product) (api.ProductResponse, error) {
	id, err := parseUUID(product.ID)
	if err != nil {
		return api.ProductResponse{}, err
	}
	sellerID, err := parseUUID(product.SellerID)
	if err != nil {
		return api.ProductResponse{}, err
	}
	return api.ProductResponse{
		Id:          id,
		Name:        product.Name,
		Description: product.Description,
		Price:       moneyFloat(product.Price),
		Stock:       product.Stock,
		Category:    product.Category,
		Status:      api.ProductStatus(product.Status),
		SellerId:    sellerID,
		CreatedAt:   product.CreatedAt,
		UpdatedAt:   product.UpdatedAt,
	}, nil
}

func toOrder(order store.Order) (api.OrderResponse, error) {
	id, err := parseUUID(order.ID)
	if err != nil {
		return api.OrderResponse{}, err
	}
	userID, err := parseUUID(order.UserID)
	if err != nil {
		return api.OrderResponse{}, err
	}
	var promoID *uuid.UUID
	if order.PromoCodeID != nil {
		parsed, err := parseUUID(*order.PromoCodeID)
		if err != nil {
			return api.OrderResponse{}, err
		}
		promoID = &parsed
	}
	items := make([]api.OrderItemResponse, 0, len(order.Items))
	for _, item := range order.Items {
		itemID, err := parseUUID(item.ID)
		if err != nil {
			return api.OrderResponse{}, err
		}
		productID, err := parseUUID(item.ProductID)
		if err != nil {
			return api.OrderResponse{}, err
		}
		items = append(items, api.OrderItemResponse{
			Id:           itemID,
			ProductId:    productID,
			Quantity:     item.Quantity,
			PriceAtOrder: moneyFloat(item.PriceAtOrder),
		})
	}
	return api.OrderResponse{
		Id:             id,
		UserId:         userID,
		Status:         api.OrderStatus(order.Status),
		PromoCodeId:    promoID,
		PromoCode:      order.PromoCode,
		TotalAmount:    moneyFloat(order.TotalAmount),
		DiscountAmount: moneyFloat(order.DiscountAmount),
		Items:          items,
		CreatedAt:      order.CreatedAt,
		UpdatedAt:      order.UpdatedAt,
	}, nil
}

func toPromo(promo store.Promo) (api.PromoCodeResponse, error) {
	id, err := parseUUID(promo.ID)
	if err != nil {
		return api.PromoCodeResponse{}, err
	}
	return api.PromoCodeResponse{
		Id:             id,
		Code:           promo.Code,
		DiscountType:   api.DiscountType(promo.DiscountType),
		DiscountValue:  moneyFloat(promo.DiscountValue),
		MinOrderAmount: moneyFloat(promo.MinOrderAmount),
		MaxUses:        promo.MaxUses,
		CurrentUses:    promo.CurrentUses,
		ValidFrom:      promo.ValidFrom,
		ValidUntil:     promo.ValidUntil,
		Active:         promo.Active,
	}, nil
}
