package store

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type Product struct {
	ID          string
	Name        string
	Description *string
	Price       decimal.Decimal
	Stock       int
	Category    string
	Status      string
	SellerID    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type ProductWrite struct {
	Name        string
	Description *string
	Price       decimal.Decimal
	Stock       int
	Category    string
	Status      string
	SellerID    string
}

func InsertProduct(ctx context.Context, q DBTX, in ProductWrite) (Product, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO products (name, description, price, stock, category, status, seller_id)
		VALUES ($1, $2, $3::numeric, $4, $5, $6::product_status, $7::uuid)
		RETURNING id::text, name, description, price::text, stock, category, status::text, seller_id::text, created_at, updated_at
	`, in.Name, in.Description, in.Price.StringFixed(2), in.Stock, in.Category, in.Status, in.SellerID)
	return scanProduct(row)
}

func UpdateProduct(ctx context.Context, q DBTX, id string, in ProductWrite) (Product, error) {
	row := q.QueryRow(ctx, `
		UPDATE products
		SET name = $2, description = $3, price = $4::numeric, stock = $5, category = $6, status = $7::product_status
		WHERE id = $1::uuid
		RETURNING id::text, name, description, price::text, stock, category, status::text, seller_id::text, created_at, updated_at
	`, id, in.Name, in.Description, in.Price.StringFixed(2), in.Stock, in.Category, in.Status)
	product, err := scanProduct(row)
	if isNoRows(err) {
		return Product{}, ErrNotFound
	}
	return product, err
}

func ArchiveProduct(ctx context.Context, q DBTX, id string) (Product, error) {
	row := q.QueryRow(ctx, `
		UPDATE products
		SET status = 'ARCHIVED'
		WHERE id = $1::uuid
		RETURNING id::text, name, description, price::text, stock, category, status::text, seller_id::text, created_at, updated_at
	`, id)
	product, err := scanProduct(row)
	if isNoRows(err) {
		return Product{}, ErrNotFound
	}
	return product, err
}

func GetProduct(ctx context.Context, q DBTX, id string) (Product, error) {
	row := q.QueryRow(ctx, `
		SELECT id::text, name, description, price::text, stock, category, status::text, seller_id::text, created_at, updated_at
		FROM products
		WHERE id = $1::uuid
	`, id)
	product, err := scanProduct(row)
	if isNoRows(err) {
		return Product{}, ErrNotFound
	}
	return product, err
}

func GetProductForUpdate(ctx context.Context, q DBTX, id string) (Product, error) {
	row := q.QueryRow(ctx, `
		SELECT id::text, name, description, price::text, stock, category, status::text, seller_id::text, created_at, updated_at
		FROM products
		WHERE id = $1::uuid
		FOR UPDATE
	`, id)
	product, err := scanProduct(row)
	if isNoRows(err) {
		return Product{}, ErrNotFound
	}
	return product, err
}

func AdjustStock(ctx context.Context, q DBTX, id string, delta int) error {
	tag, err := q.Exec(ctx, `
		UPDATE products
		SET stock = stock + $2
		WHERE id = $1::uuid AND stock + $2 >= 0
	`, id, delta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func ListProducts(ctx context.Context, q DBTX, status, category *string, limit, offset int) ([]Product, int, error) {
	var total int
	err := q.QueryRow(ctx, `
		SELECT count(*)
		FROM products
		WHERE ($1::text IS NULL OR status::text = $1)
		  AND ($2::text IS NULL OR category = $2)
	`, status, category).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := q.Query(ctx, `
		SELECT id::text, name, description, price::text, stock, category, status::text, seller_id::text, created_at, updated_at
		FROM products
		WHERE ($1::text IS NULL OR status::text = $1)
		  AND ($2::text IS NULL OR category = $2)
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, status, category, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	products := make([]Product, 0)
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		products = append(products, product)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return products, total, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProduct(row scanner) (Product, error) {
	var (
		product Product
		price   string
	)
	err := row.Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&price,
		&product.Stock,
		&product.Category,
		&product.Status,
		&product.SellerID,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if err != nil {
		return Product{}, err
	}
	product.Price, err = decimal.NewFromString(price)
	if err != nil {
		return Product{}, err
	}
	return product, nil
}
