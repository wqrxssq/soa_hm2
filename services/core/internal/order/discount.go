package order

import "github.com/shopspring/decimal"

// applyDiscount считает скидку.
// Для PERCENTAGE значение строго больше 70 не применяется: покупатель платит 100% суммы.
// Так читается формулировка «не более 70% от total_amount, в ином случае цена 100%».
func applyDiscount(total decimal.Decimal, kind string, value decimal.Decimal) (discount, payable decimal.Decimal) {
	total = total.Round(2)
	value = value.Round(2)
	switch kind {
	case "PERCENTAGE":
		if value.GreaterThan(decimal.NewFromInt(70)) {
			return decimal.Zero, total
		}
		discount = total.Mul(value).Div(decimal.NewFromInt(100)).Round(2)
	case "FIXED_AMOUNT":
		discount = value
		if discount.GreaterThan(total) {
			discount = total
		}
	default:
		return decimal.Zero, total
	}
	if discount.IsNegative() {
		discount = decimal.Zero
	}
	if discount.GreaterThan(total) {
		discount = total
	}
	return discount, total.Sub(discount)
}
