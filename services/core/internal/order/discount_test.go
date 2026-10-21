package order

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestApplyDiscount(t *testing.T) {
	cases := []struct {
		name     string
		total    string
		kind     string
		value    string
		discount string
		payable  string
	}{
		{"ten percent", "200", "PERCENTAGE", "10", "20", "180"},
		{"exactly seventy", "200", "PERCENTAGE", "70", "140", "60"},
		{"above seventy pays full price", "200", "PERCENTAGE", "70.01", "0", "200"},
		{"eighty percent pays full price", "200", "PERCENTAGE", "80", "0", "200"},
		{"fixed", "200", "FIXED_AMOUNT", "30", "30", "170"},
		{"fixed capped by total", "200", "FIXED_AMOUNT", "500", "200", "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			discount, payable := applyDiscount(decimal.RequireFromString(tc.total), tc.kind, decimal.RequireFromString(tc.value))
			if !discount.Equal(decimal.RequireFromString(tc.discount)) {
				t.Fatalf("discount = %s, want %s", discount, tc.discount)
			}
			if !payable.Equal(decimal.RequireFromString(tc.payable)) {
				t.Fatalf("payable = %s, want %s", payable, tc.payable)
			}
		})
	}
}
