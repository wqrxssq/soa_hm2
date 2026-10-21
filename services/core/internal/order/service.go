package order

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"marketplace/core/internal/apperr"
	"marketplace/core/internal/identity"
	"marketplace/core/internal/store"
)

type ItemInput struct {
	ProductID string
	Quantity  int
}

type Service struct {
	store     *store.Store
	rateLimit time.Duration
}

func NewService(st *store.Store, rateLimit time.Duration) *Service {
	return &Service{store: st, rateLimit: rateLimit}
}

func (s *Service) Create(ctx context.Context, actor identity.Actor, items []ItemInput, promoCode *string) (store.Order, error) {
	if actor.IsSeller() {
		return store.Order{}, denied()
	}
	var created store.Order
	err := s.store.WithinTx(ctx, func(ctx context.Context, q store.DBTX) error {
		if err := store.LockUser(ctx, q, actor.ID); err != nil {
			return err
		}
		now, err := store.Now(ctx, q)
		if err != nil {
			return err
		}
		// 1. Ограничение частоты создания.
		if err := s.checkRate(ctx, q, actor.ID, OpCreate, now); err != nil {
			return err
		}
		// 2. У пользователя не должно быть заказа в CREATED или PAYMENT_PENDING.
		active, err := store.HasActiveOrder(ctx, q, actor.ID)
		if err != nil {
			return err
		}
		if active {
			return apperr.New(http.StatusConflict, "ORDER_HAS_ACTIVE", "у пользователя уже есть активный заказ")
		}
		// 3–6. Каталог, остаток, резерв и снимок цены.
		snapshots, raw, err := exchangeStock(ctx, q, nil, items)
		if err != nil {
			return err
		}
		// 7. Промокод и сумма.
		var promoID *string
		var code *string
		discount := decimal.Zero
		payable := raw
		if promoCode != nil && *promoCode != "" {
			applied, err := consumePromo(ctx, q, *promoCode, raw, now)
			if err != nil {
				return err
			}
			promoID = &applied.ID
			code = &applied.Code
			discount = applied.Discount
			payable = applied.Payable
		}
		// 8. Заказ и операция CREATE_ORDER.
		created, err = store.InsertOrder(ctx, q, store.NewOrder{
			UserID:         actor.ID,
			Status:         StatusCreated,
			PromoCodeID:    promoID,
			TotalAmount:    payable,
			DiscountAmount: discount,
		})
		if err != nil {
			return err
		}
		created.PromoCode = code
		created.Items, err = insertSnapshots(ctx, q, created.ID, snapshots)
		if err != nil {
			return err
		}
		return store.InsertOperation(ctx, q, actor.ID, OpCreate)
	})
	return created, err
}

func (s *Service) Update(ctx context.Context, actor identity.Actor, orderID string, items []ItemInput) (store.Order, error) {
	if actor.IsSeller() {
		return store.Order{}, denied()
	}
	var updated store.Order
	err := s.store.WithinTx(ctx, func(ctx context.Context, q store.DBTX) error {
		ownerID, err := store.OrderUserID(ctx, q, orderID)
		if errors.Is(err, store.ErrNotFound) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		if err := store.LockUser(ctx, q, ownerID); err != nil {
			return err
		}
		current, err := store.GetOrderForUpdate(ctx, q, orderID)
		if errors.Is(err, store.ErrNotFound) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		// 1. Владелец.
		if err := ensureOwner(actor, current.UserID); err != nil {
			return err
		}
		// 2. Только CREATED.
		if !CanUpdate(current.Status) {
			return invalidTransition()
		}
		now, err := store.Now(ctx, q)
		if err != nil {
			return err
		}
		// 3. Ограничение частоты обновления.
		if err := s.checkRate(ctx, q, current.UserID, OpUpdate, now); err != nil {
			return err
		}
		// 4–5. Вернуть старые остатки и зарезервировать новые.
		snapshots, raw, err := exchangeStock(ctx, q, current.Items, items)
		if err != nil {
			return err
		}
		// 6. Пересчитать стоимость и заново проверить промокод.
		promoID, discount, payable, err := repricePromo(ctx, q, current.PromoCodeID, raw, now)
		if err != nil {
			return err
		}
		if err := store.DeleteOrderItems(ctx, q, current.ID); err != nil {
			return err
		}
		if _, err := insertSnapshots(ctx, q, current.ID, snapshots); err != nil {
			return err
		}
		updated, err = store.UpdateOrderTotals(ctx, q, current.ID, current.Status, promoID, payable, discount)
		if err != nil {
			return err
		}
		// 7. Зафиксировать UPDATE_ORDER.
		return store.InsertOperation(ctx, q, current.UserID, OpUpdate)
	})
	return updated, err
}

func (s *Service) Cancel(ctx context.Context, actor identity.Actor, orderID string) (store.Order, error) {
	if actor.IsSeller() {
		return store.Order{}, denied()
	}
	var canceled store.Order
	err := s.store.WithinTx(ctx, func(ctx context.Context, q store.DBTX) error {
		ownerID, err := store.OrderUserID(ctx, q, orderID)
		if errors.Is(err, store.ErrNotFound) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		if err := store.LockUser(ctx, q, ownerID); err != nil {
			return err
		}
		current, err := store.GetOrderForUpdate(ctx, q, orderID)
		if errors.Is(err, store.ErrNotFound) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		if err := ensureOwner(actor, current.UserID); err != nil {
			return err
		}
		if !CanCancel(current.Status) {
			return invalidTransition()
		}
		if err := restoreStock(ctx, q, current.Items); err != nil {
			return err
		}
		if current.PromoCodeID != nil {
			if err := store.DecrementPromoUses(ctx, q, *current.PromoCodeID); err != nil {
				return err
			}
		}
		canceled, err = store.UpdateOrderTotals(ctx, q, current.ID, StatusCanceled, current.PromoCodeID, current.TotalAmount, current.DiscountAmount)
		return err
	})
	return canceled, err
}

func (s *Service) Advance(ctx context.Context, actor identity.Actor, orderID string) (store.Order, error) {
	if actor.IsSeller() {
		return store.Order{}, denied()
	}
	var advanced store.Order
	err := s.store.WithinTx(ctx, func(ctx context.Context, q store.DBTX) error {
		ownerID, err := store.OrderUserID(ctx, q, orderID)
		if errors.Is(err, store.ErrNotFound) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		if err := store.LockUser(ctx, q, ownerID); err != nil {
			return err
		}
		current, err := store.GetOrderForUpdate(ctx, q, orderID)
		if errors.Is(err, store.ErrNotFound) {
			return orderNotFound()
		}
		if err != nil {
			return err
		}
		if err := ensureOwner(actor, current.UserID); err != nil {
			return err
		}
		next, ok := NextStatus(current.Status)
		if !ok {
			return invalidTransition()
		}
		advanced, err = store.UpdateOrderTotals(ctx, q, current.ID, next, current.PromoCodeID, current.TotalAmount, current.DiscountAmount)
		return err
	})
	return advanced, err
}

func (s *Service) Get(ctx context.Context, actor identity.Actor, orderID string) (store.Order, error) {
	if actor.IsSeller() {
		return store.Order{}, denied()
	}
	current, err := store.GetOrder(ctx, s.store.Query(), orderID)
	if errors.Is(err, store.ErrNotFound) {
		return store.Order{}, orderNotFound()
	}
	if err != nil {
		return store.Order{}, err
	}
	if err := ensureOwner(actor, current.UserID); err != nil {
		return store.Order{}, err
	}
	return current, nil
}

func (s *Service) checkRate(ctx context.Context, q store.DBTX, userID, operation string, now time.Time) error {
	if s.rateLimit <= 0 {
		return nil
	}
	last, err := store.LastOperationAt(ctx, q, userID, operation)
	if err != nil {
		return err
	}
	if last != nil && now.Sub(*last) < s.rateLimit {
		return apperr.New(http.StatusTooManyRequests, "ORDER_LIMIT_EXCEEDED", "превышен лимит частоты операции с заказом")
	}
	return nil
}

type appliedPromo struct {
	ID       string
	Code     string
	Discount decimal.Decimal
	Payable  decimal.Decimal
}

func consumePromo(ctx context.Context, q store.DBTX, code string, raw decimal.Decimal, now time.Time) (appliedPromo, error) {
	promo, err := store.GetPromoForUpdateByCode(ctx, q, code)
	if errors.Is(err, store.ErrNotFound) {
		return appliedPromo{}, invalidPromo()
	}
	if err != nil {
		return appliedPromo{}, err
	}
	if !promo.Active || promo.CurrentUses >= promo.MaxUses || now.Before(promo.ValidFrom) || now.After(promo.ValidUntil) {
		return appliedPromo{}, invalidPromo()
	}
	if raw.LessThan(promo.MinOrderAmount) {
		return appliedPromo{}, apperr.New(http.StatusUnprocessableEntity, "PROMO_CODE_MIN_AMOUNT", "сумма заказа ниже минимальной для промокода")
	}
	discount, payable := applyDiscount(raw, promo.DiscountType, promo.DiscountValue)
	if err := store.IncrementPromoUses(ctx, q, promo.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return appliedPromo{}, invalidPromo()
		}
		return appliedPromo{}, err
	}
	return appliedPromo{ID: promo.ID, Code: promo.Code, Discount: discount, Payable: payable}, nil
}

func repricePromo(ctx context.Context, q store.DBTX, promoID *string, raw decimal.Decimal, now time.Time) (*string, decimal.Decimal, decimal.Decimal, error) {
	if promoID == nil {
		return nil, decimal.Zero, raw, nil
	}
	promo, err := store.GetPromoForUpdateByID(ctx, q, *promoID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, decimal.Zero, decimal.Zero, invalidPromo()
	}
	if err != nil {
		return nil, decimal.Zero, decimal.Zero, err
	}
	// Использование уже учтено этим заказом, поэтому max_uses повторно не проверяется.
	if !promo.Active || now.Before(promo.ValidFrom) || now.After(promo.ValidUntil) {
		return nil, decimal.Zero, decimal.Zero, invalidPromo()
	}
	if raw.LessThan(promo.MinOrderAmount) {
		if err := store.DecrementPromoUses(ctx, q, promo.ID); err != nil {
			return nil, decimal.Zero, decimal.Zero, err
		}
		return nil, decimal.Zero, raw, nil
	}
	discount, payable := applyDiscount(raw, promo.DiscountType, promo.DiscountValue)
	return promoID, discount, payable, nil
}

type lineSnap struct {
	ProductID string
	Quantity  int
	Price     decimal.Decimal
}

func exchangeStock(ctx context.Context, q store.DBTX, old []store.OrderItem, fresh []ItemInput) ([]lineSnap, decimal.Decimal, error) {
	_, oldQty := groupItems(old)
	newOrder, newQty := groupInputs(fresh)
	ids := unionKeys(oldQty, newQty)
	sort.Strings(ids)

	locked := make(map[string]store.Product, len(ids))
	for _, id := range ids {
		product, err := store.GetProductForUpdate(ctx, q, id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, decimal.Zero, err
		}
		locked[id] = product
	}
	for _, line := range fresh {
		if _, ok := locked[line.ProductID]; !ok {
			return nil, decimal.Zero, apperr.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "товар не найден")
		}
	}
	for _, line := range fresh {
		if locked[line.ProductID].Status != "ACTIVE" {
			return nil, decimal.Zero, apperr.New(http.StatusConflict, "PRODUCT_INACTIVE", "товар неактивен")
		}
	}

	var shortages []map[string]any
	for _, id := range newOrder {
		available := locked[id].Stock + oldQty[id]
		if available < newQty[id] {
			shortages = append(shortages, map[string]any{
				"product_id": id,
				"requested":  newQty[id],
				"available":  available,
			})
		}
	}
	if len(shortages) > 0 {
		return nil, decimal.Zero, apperr.WithDetails(http.StatusConflict, "INSUFFICIENT_STOCK", "недостаточно товара на складе", map[string]any{
			"items": shortages,
		})
	}

	for _, id := range ids {
		if _, ok := locked[id]; !ok {
			return nil, decimal.Zero, apperr.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "товар не найден")
		}
		delta := oldQty[id] - newQty[id]
		if delta == 0 {
			continue
		}
		if err := store.AdjustStock(ctx, q, id, delta); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, decimal.Zero, apperr.New(http.StatusConflict, "INSUFFICIENT_STOCK", "недостаточно товара на складе")
			}
			return nil, decimal.Zero, err
		}
	}

	raw := decimal.Zero
	snapshots := make([]lineSnap, 0, len(fresh))
	for _, line := range fresh {
		price := locked[line.ProductID].Price
		raw = raw.Add(price.Mul(decimal.NewFromInt(int64(line.Quantity))))
		snapshots = append(snapshots, lineSnap{ProductID: line.ProductID, Quantity: line.Quantity, Price: price})
	}
	return snapshots, raw.Round(2), nil
}

func restoreStock(ctx context.Context, q store.DBTX, items []store.OrderItem) error {
	orderIDs, qty := groupItems(items)
	sort.Strings(orderIDs)
	for _, id := range orderIDs {
		if _, err := store.GetProductForUpdate(ctx, q, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return apperr.New(http.StatusNotFound, "PRODUCT_NOT_FOUND", "товар не найден")
			}
			return err
		}
	}
	for _, id := range orderIDs {
		if err := store.AdjustStock(ctx, q, id, qty[id]); err != nil {
			return err
		}
	}
	return nil
}

func insertSnapshots(ctx context.Context, q store.DBTX, orderID string, snapshots []lineSnap) ([]store.OrderItem, error) {
	items := make([]store.OrderItem, 0, len(snapshots))
	for _, snap := range snapshots {
		item, err := store.InsertOrderItem(ctx, q, orderID, snap.ProductID, snap.Quantity, snap.Price)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func groupItems(items []store.OrderItem) ([]string, map[string]int) {
	orderIDs := make([]string, 0)
	qty := make(map[string]int)
	for _, item := range items {
		if _, ok := qty[item.ProductID]; !ok {
			orderIDs = append(orderIDs, item.ProductID)
		}
		qty[item.ProductID] += item.Quantity
	}
	return orderIDs, qty
}

func groupInputs(items []ItemInput) ([]string, map[string]int) {
	orderIDs := make([]string, 0)
	qty := make(map[string]int)
	for _, item := range items {
		if _, ok := qty[item.ProductID]; !ok {
			orderIDs = append(orderIDs, item.ProductID)
		}
		qty[item.ProductID] += item.Quantity
	}
	return orderIDs, qty
}

func unionKeys(left, right map[string]int) []string {
	seen := make(map[string]struct{}, len(left)+len(right))
	for id := range left {
		seen[id] = struct{}{}
	}
	for id := range right {
		seen[id] = struct{}{}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

func ensureOwner(actor identity.Actor, ownerID string) error {
	if actor.IsAdmin() || actor.ID == ownerID {
		return nil
	}
	return apperr.New(http.StatusForbidden, "ORDER_OWNERSHIP_VIOLATION", "заказ принадлежит другому пользователю")
}

func denied() error {
	return apperr.New(http.StatusForbidden, "ACCESS_DENIED", "недостаточно прав")
}

func orderNotFound() error {
	return apperr.New(http.StatusNotFound, "ORDER_NOT_FOUND", "заказ не найден")
}

func invalidTransition() error {
	return apperr.New(http.StatusConflict, "INVALID_STATE_TRANSITION", "недопустимый переход состояния")
}

func invalidPromo() error {
	return apperr.New(http.StatusUnprocessableEntity, "PROMO_CODE_INVALID", "промокод недействителен")
}
