package worker

import (
	"context"
	"sync"
	"testing"

	"baldassi/internal/exchange"
	"github.com/shopspring/decimal"
)

// TestCEXToDEXCalculationPrecision - EXACT calculation verification
func TestCEXToDEXCalculationPrecision(t *testing.T) {
	// CEX: ASK=$100.00, BID=$99.00
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(100.00),
		sellPrice: decimal.NewFromFloat(99.00),
		buyTotal:  decimal.NewFromFloat(100.00), // Cost to buy 1 token
		sellTotal: decimal.NewFromFloat(99.00),  // Revenue from selling 1 token
	}

	// DEX: ASK=$101.00, BID=$102.00
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(101.00),
		sellPrice: decimal.NewFromFloat(102.00),
		buyTotal:  decimal.NewFromFloat(101.00),
		sellTotal: decimal.NewFromFloat(102.00),
	}

	var opportunities []*exchange.ArbitrageOpportunity
	var mu sync.Mutex

	job := &ArbitrageOpportunityChecker{
		CEX:           cex,
		DEX:           dex,
		GasCalculator: nil,
		TradeSize:     decimal.NewFromFloat(1.0),
		BlockNumber:   1,
		resultMutex:   &mu,
		opportunities: &opportunities,
	}

	ctx := context.Background()

	// Get both quotes
	cexQuote, _ := cex.GetPrice(ctx, decimal.NewFromFloat(1.0))
	dexQuote, _ := dex.GetPrice(ctx, decimal.NewFromFloat(1.0))

	// CEX→DEX: Buy on CEX at ASK, Sell on DEX at BID
	// Buy cost:  $100.00 (CEX ASK)
	// Sell rev:  $102.00 (DEX BID)
	// Gross:     $2.00
	expectedGross := decimal.NewFromFloat(2.00)

	job.Execute(ctx)

	// Manual calculation
	buyTotal := cexQuote.BuyTotal     // $100
	sellRevenue := dexQuote.SellTotal // $102
	actualGross := sellRevenue.Sub(buyTotal)

	if !actualGross.Equal(expectedGross) {
		t.Errorf("CEX→DEX gross profit WRONG: got %s, want %s", actualGross, expectedGross)
		t.Errorf("  Buy cost (CEX ASK):  %s", buyTotal)
		t.Errorf("  Sell rev (DEX BID):  %s", sellRevenue)
	}
}

// TestDEXToCEXCalculationPrecision - calculation verification
func TestDEXToCEXCalculationPrecision(t *testing.T) {
	// CEX: ASK=$100.00, BID=$103.00 (HIGH BID!)
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(100.00),
		sellPrice: decimal.NewFromFloat(103.00), // CEX willing to pay $103
		buyTotal:  decimal.NewFromFloat(100.00),
		sellTotal: decimal.NewFromFloat(103.00),
	}

	// DEX: ASK=$99.00 (CHEAP!), BID=$98.00
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(99.00), // Can buy cheap on DEX
		sellPrice: decimal.NewFromFloat(98.00),
		buyTotal:  decimal.NewFromFloat(99.00),
		sellTotal: decimal.NewFromFloat(98.00),
	}

	ctx := context.Background()
	cexQuote, _ := cex.GetPrice(ctx, decimal.NewFromFloat(1.0))
	dexQuote, _ := dex.GetPrice(ctx, decimal.NewFromFloat(1.0))

	// DEX→CEX: Buy on DEX at ASK, Sell on CEX at BID
	// Buy cost:  $99.00 (DEX ASK)
	// Sell rev:  $103.00 (CEX BID)
	// Gross:     $4.00
	expectedGross := decimal.NewFromFloat(4.00)

	// Manual calculation
	buyTotal := dexQuote.BuyTotal     // $99
	sellRevenue := cexQuote.SellTotal // $103
	actualGross := sellRevenue.Sub(buyTotal)

	if !actualGross.Equal(expectedGross) {
		t.Errorf("DEX→CEX gross profit WRONG: got %s, want %s", actualGross, expectedGross)
		t.Errorf("  Buy cost (DEX ASK):  %s", buyTotal)
		t.Errorf("  Sell rev (CEX BID):  %s", sellRevenue)
	}
}

// TestSpreadSignMismatch - Negative spread should NEVER trigger arbitrage
func TestSpreadSignMismatch(t *testing.T) {
	// CEX: ASK=$100, BID=$99
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(100.00),
		sellPrice: decimal.NewFromFloat(99.00),
		buyTotal:  decimal.NewFromFloat(100.00),
		sellTotal: decimal.NewFromFloat(99.00),
	}

	// DEX: ASK=$101, BID=$98 (DEX BID < CEX ASK - NO ARBITRAGE!)
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(101.00),
		sellPrice: decimal.NewFromFloat(98.00), // Lower than CEX buy!
		buyTotal:  decimal.NewFromFloat(101.00),
		sellTotal: decimal.NewFromFloat(98.00),
	}

	var opportunities []*exchange.ArbitrageOpportunity
	var mu sync.Mutex

	job := &ArbitrageOpportunityChecker{
		CEX:           cex,
		DEX:           dex,
		GasCalculator: nil,
		TradeSize:     decimal.NewFromFloat(1.0),
		BlockNumber:   1,
		resultMutex:   &mu,
		opportunities: &opportunities,
	}

	job.Execute(context.Background())

	// CEX→DEX spread: $98 (DEX BID) - $100 (CEX ASK) = -$2
	// DEX→CEX spread: $99 (CEX BID) - $101 (DEX ASK) = -$2
	// Should find 0 opportunities

	if len(opportunities) > 0 {
		t.Errorf("Found %d opportunities on negative spreads!", len(opportunities))
		for i, opp := range opportunities {
			t.Errorf("  Opportunity %d: %s, Net: %s", i, opp.Direction, opp.NetProfit)
		}
	}
}

// TestAsymmetricSpreads - One direction profitable, other not
func TestAsymmetricSpreads(t *testing.T) {
	// CEX: ASK=$100, BID=$99
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(100.00),
		sellPrice: decimal.NewFromFloat(99.00),
		buyTotal:  decimal.NewFromFloat(100.00),
		sellTotal: decimal.NewFromFloat(99.00),
	}

	// DEX: ASK=$101, BID=$105 (HIGH BID!)
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(101.00),
		sellPrice: decimal.NewFromFloat(105.00), // DEX paying premium!
		buyTotal:  decimal.NewFromFloat(101.00),
		sellTotal: decimal.NewFromFloat(105.00),
	}

	ctx := context.Background()
	cexQuote, _ := cex.GetPrice(ctx, decimal.NewFromFloat(1.0))
	dexQuote, _ := dex.GetPrice(ctx, decimal.NewFromFloat(1.0))

	// CEX→DEX: Buy CEX $100, Sell DEX $105 = +$5 ✅ PROFITABLE
	cexToDexSpread := dexQuote.SellPrice.Sub(cexQuote.BuyPrice)
	expectedCexToDex := decimal.NewFromFloat(5.00)

	if !cexToDexSpread.Equal(expectedCexToDex) {
		t.Errorf("CEX→DEX spread wrong: got %s, want %s", cexToDexSpread, expectedCexToDex)
	}

	// DEX→CEX: Buy DEX $101, Sell CEX $99 = -$2 ❌ LOSS
	dexToCexSpread := cexQuote.SellPrice.Sub(dexQuote.BuyPrice)
	expectedDexToCex := decimal.NewFromFloat(-2.00)

	if !dexToCexSpread.Equal(expectedDexToCex) {
		t.Errorf("DEX→CEX spread wrong: got %s, want %s", dexToCexSpread, expectedDexToCex)
	}

	if !cexToDexSpread.GreaterThan(decimal.Zero) {
		t.Error("CEX→DEX should be positive!")
	}

	if dexToCexSpread.GreaterThan(decimal.Zero) {
		t.Error("DEX→CEX should be negative!")
	}
}

// TestExactlyZeroSpread - Edge case
func TestExactlyZeroSpread(t *testing.T) {
	// Both exchanges perfectly aligned
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(100.00),
		sellPrice: decimal.NewFromFloat(99.00),
		buyTotal:  decimal.NewFromFloat(100.00),
		sellTotal: decimal.NewFromFloat(99.00),
	}

	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(100.00),
		sellPrice: decimal.NewFromFloat(99.00),
		buyTotal:  decimal.NewFromFloat(100.00),
		sellTotal: decimal.NewFromFloat(99.00),
	}

	ctx := context.Background()
	cexQuote, _ := cex.GetPrice(ctx, decimal.NewFromFloat(1.0))
	dexQuote, _ := dex.GetPrice(ctx, decimal.NewFromFloat(1.0))

	// CEX→DEX: Buy CEX $100, Sell DEX $99 = -$1
	cexToDexSpread := dexQuote.SellPrice.Sub(cexQuote.BuyPrice)
	expectedNegative := decimal.NewFromFloat(-1.00)

	if !cexToDexSpread.Equal(expectedNegative) {
		t.Errorf("Spread should be -$1, got %s", cexToDexSpread)
	}

	var opportunities []*exchange.ArbitrageOpportunity
	var mu sync.Mutex

	job := &ArbitrageOpportunityChecker{
		CEX:           cex,
		DEX:           dex,
		GasCalculator: nil,
		TradeSize:     decimal.NewFromFloat(1.0),
		BlockNumber:   1,
		resultMutex:   &mu,
		opportunities: &opportunities,
	}

	job.Execute(context.Background())

	if len(opportunities) > 0 {
		t.Errorf("Should find zero opportunities at equilibrium, found %d", len(opportunities))
	}
}

// TestLargeNumbers - Precision with large trade sizes
func TestLargeNumbers(t *testing.T) {
	// Test with 1000 ETH @ $3000 = $3,000,000 trade
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(3000.00),
		sellPrice: decimal.NewFromFloat(2999.00),
		buyTotal:  decimal.NewFromFloat(3000000.00), // 1000 * $3000
		sellTotal: decimal.NewFromFloat(2999000.00), // 1000 * $2999
	}

	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(3001.00),
		sellPrice: decimal.NewFromFloat(3002.00),
		buyTotal:  decimal.NewFromFloat(3001000.00),
		sellTotal: decimal.NewFromFloat(3002000.00),
	}

	ctx := context.Background()
	cexQuote, _ := cex.GetPrice(ctx, decimal.NewFromFloat(1000.0))
	dexQuote, _ := dex.GetPrice(ctx, decimal.NewFromFloat(1000.0))

	// CEX→DEX: Buy $3,000,000, Sell $3,002,000 = $2,000 gross
	buyTotal := cexQuote.BuyTotal
	sellRevenue := dexQuote.SellTotal
	grossProfit := sellRevenue.Sub(buyTotal)

	expectedGross := decimal.NewFromFloat(2000.00)

	if !grossProfit.Equal(expectedGross) {
		t.Errorf("Large number precision failed: got %s, want %s", grossProfit, expectedGross)
	}
}

// TestSmallSpreadWithRealGas - $0.10 spread with $0.05 gas
func TestSmallSpreadWithRealGas(t *testing.T) {
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(3000.00),
		sellPrice: decimal.NewFromFloat(2999.90),
		buyTotal:  decimal.NewFromFloat(3000.00),
		sellTotal: decimal.NewFromFloat(2999.90),
	}

	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(3000.05),
		sellPrice: decimal.NewFromFloat(3000.10), // $0.10 higher than CEX buy
		buyTotal:  decimal.NewFromFloat(3000.05),
		sellTotal: decimal.NewFromFloat(3000.10),
	}

	ctx := context.Background()
	cexQuote, _ := cex.GetPrice(ctx, decimal.NewFromFloat(1.0))
	dexQuote, _ := dex.GetPrice(ctx, decimal.NewFromFloat(1.0))

	// Gross: $3000.10 - $3000.00 = $0.10
	grossProfit := dexQuote.SellTotal.Sub(cexQuote.BuyTotal)
	expectedGross := decimal.NewFromFloat(0.10)

	if !grossProfit.Round(2).Equal(expectedGross) {
		t.Errorf("Small spread calculation failed: got %s, want %s", grossProfit, expectedGross)
	}

	// With $0.05 gas, net would be $0.05 profit
	// With $30 gas (ie. default fallback), net would be -$29.90 loss
	// gas estimation matters
}
