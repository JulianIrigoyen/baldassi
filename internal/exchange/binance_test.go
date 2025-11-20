package exchange

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestBinanceFeeDirection(t *testing.T) {
	tests := []struct {
		name      string
		isBuy     bool
		totalCost decimal.Decimal
		wantMore  bool
	}{
		{
			name:      "buying adds fee to cost",
			isBuy:     true,
			totalCost: decimal.NewFromInt(30000),
			wantMore:  true,
		},
		{
			name:      "selling subtracts fee from revenue",
			isBuy:     false,
			totalCost: decimal.NewFromInt(30000),
			wantMore:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := tt.totalCost.Mul(decimal.NewFromFloat(binanceTradingFee))

			var finalTotal decimal.Decimal
			if tt.isBuy {
				finalTotal = tt.totalCost.Add(fee)
			} else {
				finalTotal = tt.totalCost.Sub(fee)
			}

			if tt.wantMore {
				if !finalTotal.GreaterThan(tt.totalCost) {
					t.Errorf("buy should increase total: got %v, base %v", finalTotal, tt.totalCost)
				}
			} else {
				if !finalTotal.LessThan(tt.totalCost) {
					t.Errorf("sell should decrease total: got %v, base %v", finalTotal, tt.totalCost)
				}
			}
		})
	}
}
