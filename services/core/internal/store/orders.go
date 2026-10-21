package store

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type Order struct {
	ID             string
	UserID         string
	Status         string
	PromoCodeID    *string
	PromoCode      *string
	TotalAmount    decimal.Decimal
	DiscountAmount decimal.Decimal
	Items          []OrderItem
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type OrderItem struct {
	ID           string
	ProductID    string
	Quantity     int
	PriceAtOrder decimal.Decimal
}

type NewOrder struct {
	UserID         string
	Status         string
	PromoCodeID    *string
	TotalAmount    decimal.Decimal
	DiscountAmount decimal.Decimal
}

func LockUser(ctx context.Context, q DBTX, userID string) error {
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID)
	return err
}

func Now(ctx context.Context, q DBTX) (time.Time, error) {
	var now time.Time
	err := q.QueryRow(ctx, `SELECT now()`).Scan(&now)
	return now, err
}

func LastOperationAt(ctx context.Context, q DBTX, userID, operationType string) (*time.Time, error) {
	var created time.Time
	err := q.QueryRow(ctx, `
		SELECT created_at
		FROM user_operations
		WHERE user_id = $1::uuid AND operation_type = $2::operation_type
		ORDER BY created_at DESC
		LIMIT 1
	`, userID, operationType).Scan(&created)
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func HasActiveOrder(ctx context.Context, q DBTX, userID string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM orders
			WHERE user_id = $1::uuid
			  AND status IN ('CREATED', 'PAYMENT_PENDING')
		)
	`, userID).Scan(&exists)
	return exists, err
}

func InsertOperation(ctx context.Context, q DBTX, userID, operationType string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO user_operations (user_id, operation_type)
		VALUES ($1::uuid, $2::operation_type)
	`, userID, operationType)
	return err
}

func InsertOrder(ctx context.Context, q DBTX, in NewOrder) (Order, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO orders (user_id, status, promo_code_id, total_amount, discount_amount)
		VALUES ($1::uuid, $2::order_status, $3::uuid, $4::numeric, $5::numeric)
		RETURNING id::text, user_id::text, status::text, promo_code_id::text, NULL::text,
		          total_amount::text, discount_amount::text, created_at, updated_at
	`, in.UserID, in.Status, in.PromoCodeID, in.TotalAmount.StringFixed(2), in.DiscountAmount.StringFixed(2))
	return scanOrder(row)
}

func InsertOrderItem(ctx context.Context, q DBTX, orderID, productID string, quantity int, price decimal.Decimal) (OrderItem, error) {
	var item OrderItem
	var priceText string
	err := q.QueryRow(ctx, `
		INSERT INTO order_items (order_id, product_id, quantity, price_at_order)
		VALUES ($1::uuid, $2::uuid, $3, $4::numeric)
		RETURNING id::text, product_id::text, quantity, price_at_order::text
	`, orderID, productID, quantity, price.StringFixed(2)).Scan(&item.ID, &item.ProductID, &item.Quantity, &priceText)
	if err != nil {
		return OrderItem{}, err
	}
	item.PriceAtOrder, err = decimal.NewFromString(priceText)
	return item, err
}

func DeleteOrderItems(ctx context.Context, q DBTX, orderID string) error {
	_, err := q.Exec(ctx, `DELETE FROM order_items WHERE order_id = $1::uuid`, orderID)
	return err
}

func OrderUserID(ctx context.Context, q DBTX, orderID string) (string, error) {
	var userID string
	err := q.QueryRow(ctx, `SELECT user_id::text FROM orders WHERE id = $1::uuid`, orderID).Scan(&userID)
	if isNoRows(err) {
		return "", ErrNotFound
	}
	return userID, err
}

func GetOrderForUpdate(ctx context.Context, q DBTX, orderID string) (Order, error) {
	row := q.QueryRow(ctx, `
		SELECT o.id::text, o.user_id::text, o.status::text, o.promo_code_id::text, p.code,
		       o.total_amount::text, o.discount_amount::text, o.created_at, o.updated_at
		FROM orders o
		LEFT JOIN promo_codes p ON p.id = o.promo_code_id
		WHERE o.id = $1::uuid
		FOR UPDATE OF o
	`, orderID)
	order, err := scanOrder(row)
	if isNoRows(err) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	order.Items, err = listItems(ctx, q, orderID)
	return order, err
}

func GetOrder(ctx context.Context, q DBTX, orderID string) (Order, error) {
	row := q.QueryRow(ctx, `
		SELECT o.id::text, o.user_id::text, o.status::text, o.promo_code_id::text, p.code,
		       o.total_amount::text, o.discount_amount::text, o.created_at, o.updated_at
		FROM orders o
		LEFT JOIN promo_codes p ON p.id = o.promo_code_id
		WHERE o.id = $1::uuid
	`, orderID)
	order, err := scanOrder(row)
	if isNoRows(err) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	order.Items, err = listItems(ctx, q, orderID)
	return order, err
}

func UpdateOrderTotals(ctx context.Context, q DBTX, orderID, status string, promoID *string, total, discount decimal.Decimal) (Order, error) {
	row := q.QueryRow(ctx, `
		UPDATE orders
		SET status = $2::order_status,
		    promo_code_id = $3::uuid,
		    total_amount = $4::numeric,
		    discount_amount = $5::numeric
		WHERE id = $1::uuid
		RETURNING id::text, user_id::text, status::text, promo_code_id::text, NULL::text,
		          total_amount::text, discount_amount::text, created_at, updated_at
	`, orderID, status, promoID, total.StringFixed(2), discount.StringFixed(2))
	order, err := scanOrder(row)
	if isNoRows(err) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if promoID != nil {
		err = q.QueryRow(ctx, `SELECT code FROM promo_codes WHERE id = $1::uuid`, *promoID).Scan(&order.PromoCode)
		if err != nil {
			return Order{}, err
		}
	}
	order.Items, err = listItems(ctx, q, orderID)
	return order, err
}

func listItems(ctx context.Context, q DBTX, orderID string) ([]OrderItem, error) {
	rows, err := q.Query(ctx, `
		SELECT id::text, product_id::text, quantity, price_at_order::text
		FROM order_items
		WHERE order_id = $1::uuid
		ORDER BY id
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]OrderItem, 0)
	for rows.Next() {
		var item OrderItem
		var price string
		if err := rows.Scan(&item.ID, &item.ProductID, &item.Quantity, &price); err != nil {
			return nil, err
		}
		item.PriceAtOrder, err = decimal.NewFromString(price)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanOrder(row scanner) (Order, error) {
	var (
		order    Order
		total    string
		discount string
	)
	err := row.Scan(
		&order.ID,
		&order.UserID,
		&order.Status,
		&order.PromoCodeID,
		&order.PromoCode,
		&total,
		&discount,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if err != nil {
		return Order{}, err
	}
	order.TotalAmount, err = decimal.NewFromString(total)
	if err != nil {
		return Order{}, err
	}
	order.DiscountAmount, err = decimal.NewFromString(discount)
	if err != nil {
		return Order{}, err
	}
	return order, nil
}
