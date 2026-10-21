package store

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type Promo struct {
	ID             string
	Code           string
	DiscountType   string
	DiscountValue  decimal.Decimal
	MinOrderAmount decimal.Decimal
	MaxUses        int
	CurrentUses    int
	ValidFrom      time.Time
	ValidUntil     time.Time
	Active         bool
}

type PromoWrite struct {
	Code           string
	DiscountType   string
	DiscountValue  decimal.Decimal
	MinOrderAmount decimal.Decimal
	MaxUses        int
	ValidFrom      time.Time
	ValidUntil     time.Time
	Active         bool
}

func InsertPromo(ctx context.Context, q DBTX, in PromoWrite) (Promo, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO promo_codes (
			code, discount_type, discount_value, min_order_amount, max_uses, valid_from, valid_until, active
		)
		VALUES ($1, $2::discount_type, $3::numeric, $4::numeric, $5, $6, $7, $8)
		RETURNING id::text, code, discount_type::text, discount_value::text, min_order_amount::text,
		          max_uses, current_uses, valid_from, valid_until, active
	`, in.Code, in.DiscountType, in.DiscountValue.StringFixed(2), in.MinOrderAmount.StringFixed(2), in.MaxUses, in.ValidFrom, in.ValidUntil, in.Active)
	promo, err := scanPromo(row)
	if isUnique(err) {
		return Promo{}, errUnique("code")
	}
	return promo, err
}

func GetPromoForUpdateByCode(ctx context.Context, q DBTX, code string) (Promo, error) {
	row := q.QueryRow(ctx, `
		SELECT id::text, code, discount_type::text, discount_value::text, min_order_amount::text,
		       max_uses, current_uses, valid_from, valid_until, active
		FROM promo_codes
		WHERE code = $1
		FOR UPDATE
	`, code)
	promo, err := scanPromo(row)
	if isNoRows(err) {
		return Promo{}, ErrNotFound
	}
	return promo, err
}

func GetPromoForUpdateByID(ctx context.Context, q DBTX, id string) (Promo, error) {
	row := q.QueryRow(ctx, `
		SELECT id::text, code, discount_type::text, discount_value::text, min_order_amount::text,
		       max_uses, current_uses, valid_from, valid_until, active
		FROM promo_codes
		WHERE id = $1::uuid
		FOR UPDATE
	`, id)
	promo, err := scanPromo(row)
	if isNoRows(err) {
		return Promo{}, ErrNotFound
	}
	return promo, err
}

func IncrementPromoUses(ctx context.Context, q DBTX, id string) error {
	tag, err := q.Exec(ctx, `
		UPDATE promo_codes
		SET current_uses = current_uses + 1
		WHERE id = $1::uuid AND current_uses < max_uses
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func DecrementPromoUses(ctx context.Context, q DBTX, id string) error {
	tag, err := q.Exec(ctx, `
		UPDATE promo_codes
		SET current_uses = current_uses - 1
		WHERE id = $1::uuid AND current_uses > 0
	`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanPromo(row scanner) (Promo, error) {
	var (
		promo   Promo
		value   string
		minimum string
	)
	err := row.Scan(
		&promo.ID,
		&promo.Code,
		&promo.DiscountType,
		&value,
		&minimum,
		&promo.MaxUses,
		&promo.CurrentUses,
		&promo.ValidFrom,
		&promo.ValidUntil,
		&promo.Active,
	)
	if err != nil {
		return Promo{}, err
	}
	promo.DiscountValue, err = decimal.NewFromString(value)
	if err != nil {
		return Promo{}, err
	}
	promo.MinOrderAmount, err = decimal.NewFromString(minimum)
	if err != nil {
		return Promo{}, err
	}
	return promo, nil
}
