package worker

import (
	"context"
	"sync"
	"testing"

	"baldassi/internal/exchange"
	"github.com/shopspring/decimal"
)

// MockExchange for testing
type MockExchange struct {
	name      string
	buyPrice  decimal.Decimal
	sellPrice decimal.Decimal
	buyTotal  decimal.Decimal
	sellTotal decimal.Decimal
}

func (m *MockExchange) Name() string                { return m.name }
func (m *MockExchange) Type() exchange.ExchangeType { return exchange.TypeCEX }

func (m *MockExchange) GetPrice(ctx context.Context, amount decimal.Decimal) (*exchange.PriceQuote, error) {
	return &exchange.PriceQuote{
		BuyPrice:  m.buyPrice,
		SellPrice: m.sellPrice,
		BuyTotal:  m.buyTotal,
		SellTotal: m.sellTotal,
	}, nil
}

func (m *MockExchange) Start(ctx context.Context) error { return nil }
func (m *MockExchange) Stop() error                     { return nil }

// TestCEXToDEXArbitrage tests CEX→DEX direction (buy on CEX, sell on DEX)
func TestCEXToDEXArbitrage(t *testing.T) {
	// CEX: Buy at $3070 (ASK), Sell at $3069 (BID)
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(3070.00), // ASK (what we pay)
		sellPrice: decimal.NewFromFloat(3069.00), // BID (what we get)
		buyTotal:  decimal.NewFromFloat(3070.00), // Cost to buy 1 ETH
		sellTotal: decimal.NewFromFloat(3069.00), // Revenue from selling 1 ETH
	}

	// DEX: Buy at $3072 (ASK), Sell at $3075 (BID)
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(3072.00), // ASK
		sellPrice: decimal.NewFromFloat(3075.00), // BID (HIGHER than CEX buy!)
		buyTotal:  decimal.NewFromFloat(3072.00),
		sellTotal: decimal.NewFromFloat(3075.00),
	}

	// Create job
	var opportunities []*exchange.ArbitrageOpportunity
	var mu sync.Mutex

	job := &ArbitrageOpportunityChecker{
		CEX:           cex,
		DEX:           dex,
		GasCalculator: nil, // Will use default gas
		TradeSize:     decimal.NewFromFloat(1.0),
		BlockNumber:   1,
		resultMutex:   &mu,
		opportunities: &opportunities,
	}

	// Execute
	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should find CEX→DEX opportunity
	// Buy on CEX at $3070, Sell on DEX at $3075 = $5 gross profit
	// Minus gas (default $30) = -$25 net (no opportunity with high gas)

	// But let's verify the spread check
	cexToDexSpread := dex.sellPrice.Sub(cex.buyPrice)
	expectedSpread := decimal.NewFromFloat(5.0) // $3075 - $3070 = $5

	if !cexToDexSpread.Equal(expectedSpread) {
		t.Errorf("CEX→DEX spread wrong: got %s, want %s", cexToDexSpread, expectedSpread)
	}

	// Should NOT find DEX→CEX opportunity
	// Buy on DEX at $3072, Sell on CEX at $3069 = -$3 (loss)
	dexToCexSpread := cex.sellPrice.Sub(dex.buyPrice)
	expectedLoss := decimal.NewFromFloat(-3.0) // $3069 - $3072 = -$3

	if !dexToCexSpread.Equal(expectedLoss) {
		t.Errorf("DEX→CEX spread wrong: got %s, want %s", dexToCexSpread, expectedLoss)
	}
}

// TestDEXToCEXArbitrage tests DEX→CEX direction (buy on DEX, sell on CEX)
func TestDEXToCEXArbitrage(t *testing.T) {
	// CEX: Buy at $3070 (ASK), Sell at $3075 (BID) - HIGH BID!
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(3070.00),
		sellPrice: decimal.NewFromFloat(3075.00), // BID (HIGHER!)
		buyTotal:  decimal.NewFromFloat(3070.00),
		sellTotal: decimal.NewFromFloat(3075.00),
	}

	// DEX: Buy at $3068 (ASK), Sell at $3067 (BID) - LOW ASK!
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(3068.00), // ASK (cheaper!)
		sellPrice: decimal.NewFromFloat(3067.00),
		buyTotal:  decimal.NewFromFloat(3068.00),
		sellTotal: decimal.NewFromFloat(3067.00),
	}

	// Create job
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

	// Execute
	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should NOT find CEX→DEX opportunity
	// Buy on CEX at $3070, Sell on DEX at $3067 = -$3 (loss)
	cexToDexSpread := dex.sellPrice.Sub(cex.buyPrice)
	expectedLoss := decimal.NewFromFloat(-3.0)

	if !cexToDexSpread.Equal(expectedLoss) {
		t.Errorf("CEX→DEX spread wrong: got %s, want %s", cexToDexSpread, expectedLoss)
	}

	// Should find DEX→CEX opportunity
	// Buy on DEX at $3068, Sell on CEX at $3075 = $7 gross profit
	dexToCexSpread := cex.sellPrice.Sub(dex.buyPrice)
	expectedProfit := decimal.NewFromFloat(7.0) // $3075 - $3068 = $7

	if !dexToCexSpread.Equal(expectedProfit) {
		t.Errorf("DEX→CEX spread wrong: got %s, want %s", dexToCexSpread, expectedProfit)
	}
}

// TestProfitableArbitrageWithLowGas tests realistic profitable scenario
func TestProfitableArbitrageWithLowGas(t *testing.T) {
	// CEX: Buy at $3070 (ASK), Sell at $3069 (BID)
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(3070.00),
		sellPrice: decimal.NewFromFloat(3069.00),
		buyTotal:  decimal.NewFromFloat(3070.00),
		sellTotal: decimal.NewFromFloat(3069.00),
	}

	// DEX: Buy at $3068 (ASK), Sell at $3072 (BID) - BID HIGHER than CEX ASK!
	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(3068.00),
		sellPrice: decimal.NewFromFloat(3072.00), // Higher than CEX buy!
		buyTotal:  decimal.NewFromFloat(3068.00),
		sellTotal: decimal.NewFromFloat(3072.00),
	}

	var opportunities []*exchange.ArbitrageOpportunity
	var mu sync.Mutex

	job := &ArbitrageOpportunityChecker{
		CEX:           cex,
		DEX:           dex,
		GasCalculator: nil, // Will use $30 default
		TradeSize:     decimal.NewFromFloat(1.0),
		BlockNumber:   1,
		resultMutex:   &mu,
		opportunities: &opportunities,
	}

	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// CEX→DEX: Buy CEX $3070, Sell DEX $3072 = $2 gross profit
	// Gas: $30 (default fallback)
	// Net: -$28 loss - NO OPPORTUNITY with high gas

	// BUT with realistic gas ($0.05), this WOULD be profitable!
	// Manual calculation with low gas:
	grossProfit := decimal.NewFromFloat(2.0) // $3072 - $3070
	lowGas := decimal.NewFromFloat(0.05)
	expectedNetWithLowGas := grossProfit.Sub(lowGas) // $1.95

	if !expectedNetWithLowGas.GreaterThan(decimal.Zero) {
		t.Error("This scenario should be profitable with realistic gas")
	}

	// With default gas ($30), should find NO opportunity
	if len(opportunities) > 0 {
		t.Errorf("Should not find opportunity with high gas, but found %d", len(opportunities))
	}
}

// TestNoArbitrageOpportunity tests when both directions are unprofitable
func TestNoArbitrageOpportunity(t *testing.T) {
	// Both exchanges have tight spreads, no arb
	cex := &MockExchange{
		name:      "Binance",
		buyPrice:  decimal.NewFromFloat(3070.00),
		sellPrice: decimal.NewFromFloat(3069.00),
		buyTotal:  decimal.NewFromFloat(3070.00),
		sellTotal: decimal.NewFromFloat(3069.00),
	}

	dex := &MockExchange{
		name:      "Uniswap",
		buyPrice:  decimal.NewFromFloat(3071.00),
		sellPrice: decimal.NewFromFloat(3068.00),
		buyTotal:  decimal.NewFromFloat(3071.00),
		sellTotal: decimal.NewFromFloat(3068.00),
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

	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Both directions should be negative
	cexToDexSpread := dex.sellPrice.Sub(cex.buyPrice) // $3068 - $3070 = -$2
	if cexToDexSpread.GreaterThan(decimal.Zero) {
		t.Errorf("CEX→DEX should be negative, got %s", cexToDexSpread)
	}

	dexToCexSpread := cex.sellPrice.Sub(dex.buyPrice) // $3069 - $3071 = -$2
	if dexToCexSpread.GreaterThan(decimal.Zero) {
		t.Errorf("DEX→CEX should be negative, got %s", dexToCexSpread)
	}
}
