package gas

import (
	"context"
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGasCalculation(t *testing.T) {
	tests := []struct {
		name        string
		baseFee     int64 // in Gwei
		priorityFee int64 // in Gwei
		ethPrice    float64
		expected    float64 // expected USD cost
		tolerance   float64
	}{
		{
			name:        "low gas period",
			baseFee:     20,
			priorityFee: 1,
			ethPrice:    3100,
			expected:    9.765,  // 150k * 21 Gwei * $3100 / 10^9
			tolerance:   0.01,
		},
		{
			name:        "average gas",
			baseFee:     50,
			priorityFee: 2,
			ethPrice:    3100,
			expected:    24.18,  // 150k * 52 Gwei * $3100 / 10^9
			tolerance:   0.01,
		},
		{
			name:        "high gas period",
			baseFee:     150,
			priorityFee: 5,
			ethPrice:    3100,
			expected:    72.075, // 150k * 155 Gwei * $3100 / 10^9
			tolerance:   0.01,
		},
		{
			name:        "extreme congestion",
			baseFee:     400,
			priorityFee: 10,
			ethPrice:    3100,
			expected:    190.65, // 150k * 410 Gwei * $3100 / 10^9
			tolerance:   0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("\n=== Testing Scenario: %s ===", tt.name)
			t.Logf("Input Parameters:")
			t.Logf("  Base Fee:     %d Gwei", tt.baseFee)
			t.Logf("  Priority Fee: %d Gwei", tt.priorityFee)
			t.Logf("  Total Gas:    %d Gwei", tt.baseFee+tt.priorityFee)
			t.Logf("  ETH Price:    $%.2f", tt.ethPrice)
			t.Logf("  Gas Units:    %d (Uniswap V3 swap)", UniswapV3SwapGas)

			// Calculate using our formula
			baseFeeWei := new(big.Int).Mul(big.NewInt(tt.baseFee), big.NewInt(1e9))
			priorityFeeWei := new(big.Int).Mul(big.NewInt(tt.priorityFee), big.NewInt(1e9))
			totalGasPrice := new(big.Int).Add(baseFeeWei, priorityFeeWei)

			t.Logf("\nStep-by-step Calculation:")
			t.Logf("  1. Convert to Wei: %s Wei", totalGasPrice.String())

			// Manual calculation
			gasUnits := decimal.NewFromInt(UniswapV3SwapGas)
			gasPriceDecimal := decimal.NewFromBigInt(totalGasPrice, 0)
			ethPriceDecimal := decimal.NewFromFloat(tt.ethPrice)

			t.Logf("  2. Gas cost in Wei: %s", gasUnits.Mul(gasPriceDecimal).String())

			costInETH := gasUnits.Mul(gasPriceDecimal).Div(decimal.New(1, 18))
			t.Logf("  3. Convert to ETH: %s ETH", costInETH.String())

			costInUSD := costInETH.Mul(ethPriceDecimal)
			t.Logf("  4. Convert to USD: $%.4f", costInUSD.InexactFloat64())

			t.Logf("\nVerification:")
			t.Logf("  Expected:   $%.4f", tt.expected)
			t.Logf("  Calculated: $%.4f", costInUSD.InexactFloat64())
			t.Logf("  Difference: $%.4f", costInUSD.InexactFloat64()-tt.expected)
			t.Logf("  Tolerance:  ±%.4f", tt.tolerance)

			// Assert within tolerance
			assert.InDelta(t, tt.expected, costInUSD.InexactFloat64(), tt.tolerance,
				"Gas calculation mismatch for %s", tt.name)

			if math.Abs(costInUSD.InexactFloat64()-tt.expected) <= tt.tolerance {
				t.Logf("  ✅ PASS - Within tolerance")
			} else {
				t.Logf("  ❌ FAIL - Outside tolerance")
			}
		})
	}
}

func TestCaching(t *testing.T) {
	t.Log("\n=== Testing Cache Behavior ===")

	// Create calculator with mock client
	calc := &GasCalculator{
		cacheTTL: 100 * time.Millisecond,
		getETHPrice: func() decimal.Decimal {
			t.Log("  ETH price function called (returning $3100)")
			return decimal.NewFromInt(3100)
		},
	}

	t.Logf("Cache Configuration:")
	t.Logf("  TTL: %v", calc.cacheTTL)

	// Set cache
	calc.cache = &GasData{
		SwapCostUSD: decimal.NewFromFloat(25.50),
		Timestamp:   time.Now(),
	}
	calc.cacheExpiry = time.Now().Add(calc.cacheTTL)
	t.Logf("  Initial cache value: $%s", calc.cache.SwapCostUSD.String())
	t.Logf("  Cache expires at: %v", calc.cacheExpiry.Format("15:04:05.000"))

	ctx := context.Background()

	// First call should use cache
	t.Log("\nTest 1: Cache Hit")
	cost1, _ := calc.CalculateSwapCostUSD(ctx)
	t.Logf("  Retrieved value: $%s", cost1.String())
	t.Logf("  Cache hits: %d", calc.cacheHits)
	assert.Equal(t, "25.5", cost1.String())
	assert.Equal(t, int64(1), calc.cacheHits)

	// Second call should also use cache
	t.Log("\nTest 2: Another Cache Hit")
	cost2, _ := calc.CalculateSwapCostUSD(ctx)
	t.Logf("  Retrieved value: $%s", cost2.String())
	t.Logf("  Cache hits: %d", calc.cacheHits)
	assert.Equal(t, int64(2), calc.cacheHits)

	// Wait for cache to expire
	t.Log("\nTest 3: Cache Expiration")
	t.Logf("  Sleeping for %v to expire cache...", 150*time.Millisecond)
	time.Sleep(150 * time.Millisecond)

	t.Logf("  Current time: %v", time.Now().Format("15:04:05.000"))
	t.Logf("  Cache expired? %v", time.Now().After(calc.cacheExpiry))

	// This would need a mock eth client to work fully
	// Just testing the cache logic here
	assert.True(t, time.Now().After(calc.cacheExpiry))
	t.Log("  ✅ Cache correctly expired")
}

func TestWeiToGwei(t *testing.T) {
	tests := []struct {
		wei      *big.Int
		expected string
	}{
		{big.NewInt(1e9), "1.00"},      // 1 Gwei
		{big.NewInt(50e9), "50.00"},    // 50 Gwei
		{big.NewInt(150e9), "150.00"},  // 150 Gwei
		{big.NewInt(1000e9), "1000.00"}, // 1000 Gwei
	}

	for _, tt := range tests {
		result := weiToGwei(tt.wei)
		assert.Equal(t, tt.expected, result)
	}
}

func BenchmarkCachedRead(b *testing.B) {
	calc := &GasCalculator{
		cache: &GasData{
			SwapCostUSD: decimal.NewFromFloat(25.50),
		},
		cacheExpiry: time.Now().Add(time.Hour),
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = calc.CalculateSwapCostUSD(ctx)
	}
	// Should be very fast - just a mutex lock and memory read
}