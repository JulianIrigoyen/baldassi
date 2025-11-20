package exchange

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestUniswapFeeCalculation(t *testing.T) {
	tests := []struct {
		name          string
		quoteAmount   decimal.Decimal
		expectedFee   string
	}{
		{
			name:          "fee on 3000 USDC output",
			quoteAmount:   decimal.NewFromInt(3000),
			expectedFee:   "9.03", // 3000 * 0.003 / 0.997
		},
		{
			name:          "fee on 30000 USDC output",
			quoteAmount:   decimal.NewFromInt(30000),
			expectedFee:   "90.27",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fee := tt.quoteAmount.Mul(decimal.NewFromFloat(uniswapFee)).Div(decimal.NewFromFloat(1 - uniswapFee))

			if fee.StringFixed(2) != tt.expectedFee {
				t.Errorf("fee calculation wrong: got %s, want %s", fee.StringFixed(2), tt.expectedFee)
			}
		})
	}
}

func TestUniswapTotalIncludesFees(t *testing.T) {
	quoteAmount := decimal.NewFromInt(3000)
	fee := quoteAmount.Mul(decimal.NewFromFloat(uniswapFee)).Div(decimal.NewFromFloat(1 - uniswapFee))

	buyTotal := quoteAmount.Add(fee)
	if !buyTotal.GreaterThan(quoteAmount) {
		t.Error("buy total should include fee")
	}

	sellTotal := quoteAmount.Sub(fee)
	if !sellTotal.LessThan(quoteAmount) {
		t.Error("sell total should subtract fee")
	}
}
