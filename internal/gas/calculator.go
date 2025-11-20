package gas

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"baldassi/internal/ethereum"
	"baldassi/internal/logger"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
)

const (
	// Uniswap V3 swap gas usage (actual measured value)
	UniswapV3SwapGas = 150000

	// Wei to Gwei conversion
	WeiPerGwei = 1e9

	// Default cache TTL (slightly less than block time)
	DefaultCacheTTL = 12 * time.Second
)

// GasCalculator calculates gas costs based on network data
type GasCalculator struct {
	ethClient *ethereum.RpcManager
	logger    zerolog.Logger

	// Function to get current ETH price from our exchanges
	getETHPrice func() decimal.Decimal

	// Cached calculations
	cache       *GasData
	cacheExpiry time.Time
	cacheTTL    time.Duration
	mu          sync.RWMutex

	// Performance metrics
	lastCalculationTime time.Duration
	calculationCount    int64
	cacheHits           int64
}

// GasData contains calculated gas prices and costs
type GasData struct {
	BaseFee       *big.Int        // Base fee in Wei
	PriorityFee   *big.Int        // Priority fee in Wei
	TotalGasPrice *big.Int        // Base + Priority in Wei
	BlockNumber   uint64          // Block number this data is from
	SwapCostUSD   decimal.Decimal // Calculated swap cost in USD
	Timestamp     time.Time       // When this was calculated
}

// NewGasCalculator creates a new gas calculator with RpcManager for resilience
func NewGasCalculator(ethClient *ethereum.RpcManager, ethPriceFunc func() decimal.Decimal) *GasCalculator {
	return &GasCalculator{
		ethClient:   ethClient,
		logger:      logger.WithComponent("gas"),
		getETHPrice: ethPriceFunc,
		cacheTTL:    DefaultCacheTTL,
	}
}

// CalculateSwapCostUSD returns the exact cost of a Uniswap V3 swap in USD
func (g *GasCalculator) CalculateSwapCostUSD(ctx context.Context) (decimal.Decimal, error) {
	// Check cache first
	g.mu.RLock()
	if g.cache != nil && time.Now().Before(g.cacheExpiry) {
		cost := g.cache.SwapCostUSD
		g.cacheHits++
		g.mu.RUnlock()
		return cost, nil
	}
	g.mu.RUnlock()

	// Calculate fresh data
	gasData, err := g.calculateGasData(ctx)
	if err != nil {
		return decimal.Zero, fmt.Errorf("failed to calculate gas data: %w", err)
	}

	return gasData.SwapCostUSD, nil
}

// GetCurrentGasData returns detailed gas calculation data
func (g *GasCalculator) GetCurrentGasData(ctx context.Context) (*GasData, error) {
	// Check cache
	g.mu.RLock()
	if g.cache != nil && time.Now().Before(g.cacheExpiry) {
		data := g.cache
		g.mu.RUnlock()
		return data, nil
	}
	g.mu.RUnlock()

	return g.calculateGasData(ctx)
}

// UpdateOnBlock updates gas calculations for a new block (async)
func (g *GasCalculator) UpdateOnBlock(ctx context.Context, blockNumber uint64) {
	go func() {
		start := time.Now()
		_, err := g.calculateGasData(ctx)
		if err != nil {
			g.logger.Error().
				Err(err).
				Uint64("block", blockNumber).
				Msg("Failed to update gas data")
		} else {
			g.mu.Lock()
			g.lastCalculationTime = time.Since(start)
			g.calculationCount++
			g.mu.Unlock()
		}
	}()
}

// calculateGasData performs the actual calculation
func (g *GasCalculator) calculateGasData(ctx context.Context) (*GasData, error) {
	start := time.Now()

	// OPTIMIZED: Use SuggestGasPrice instead of fetching entire block
	// This is much faster (1-2 RPC calls vs. fetching full block data)

	// Get suggested gas price (base + priority)
	gasPrice, err := g.ethClient.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	// Get suggested priority fee (tip)
	priorityFee, err := g.ethClient.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get priority fee: %w", err)
	}

	// Calculate base fee (total - priority)
	baseFee := new(big.Int).Sub(gasPrice, priorityFee)
	if baseFee.Sign() < 0 {
		baseFee = big.NewInt(0)
	}

	// Get current block number (lightweight call)
	blockNumber, err := g.ethClient.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get block number: %w", err)
	}

	// Calculate total gas price (base + priority)
	totalGasPrice := gasPrice

	// Get current ETH price
	ethPrice := g.getETHPrice()
	if ethPrice.IsZero() || ethPrice.IsNegative() {
		return nil, fmt.Errorf("invalid ETH price: %s (price must be available from exchanges)", ethPrice.String())
	}

	// Calculate swap cost:
	// Cost (USD) = GasUnits * GasPrice(Wei) * ETHPrice(USD) / 10^18
	gasUnits := decimal.NewFromInt(UniswapV3SwapGas)
	gasPriceDecimal := decimal.NewFromBigInt(totalGasPrice, 0)

	// Convert from Wei to ETH (divide by 10^18)
	costInETH := gasUnits.Mul(gasPriceDecimal).Div(decimal.New(1, 18))

	// Convert to USD
	costInUSD := costInETH.Mul(ethPrice)

	// Create gas data
	gasData := &GasData{
		BaseFee:       baseFee,
		PriorityFee:   priorityFee,
		TotalGasPrice: totalGasPrice,
		BlockNumber:   blockNumber,
		SwapCostUSD:   costInUSD,
		Timestamp:     time.Now(),
	}

	// Update cache
	g.mu.Lock()
	g.cache = gasData
	g.cacheExpiry = time.Now().Add(g.cacheTTL)
	g.lastCalculationTime = time.Since(start)
	g.calculationCount++
	g.mu.Unlock()

	// Debug logging with all components
	g.logger.Debug().
		Str("component", "gas").
		Uint64("block", gasData.BlockNumber).
		Str("base_fee_gwei", weiToGwei(baseFee)).
		Str("priority_fee_gwei", weiToGwei(priorityFee)).
		Int64("gas_limit", UniswapV3SwapGas).
		Float64("eth_price_usd", ethPrice.InexactFloat64()).
		Float64("total_gas_eth", costInETH.InexactFloat64()).
		Float64("swap_cost_usd", costInUSD.InexactFloat64()).
		Dur("calc_time", g.lastCalculationTime).
		Msg("Gas data updated (detailed)")

	return gasData, nil
}

// GetMetrics returns performance metrics
func (g *GasCalculator) GetMetrics() (cacheHits, calculations int64, avgCalcTime time.Duration) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.calculationCount > 0 {
		avgCalcTime = g.lastCalculationTime // Could track average if needed
	}
	return g.cacheHits, g.calculationCount, avgCalcTime
}

// weiToGwei converts Wei to Gwei for display (with decimals)
func weiToGwei(wei *big.Int) string {
	if wei == nil {
		return "0.00"
	}
	// Convert to decimal for proper display with 2 decimal places
	weiDecimal := decimal.NewFromBigInt(wei, 0)
	gweiDecimal := weiDecimal.Div(decimal.NewFromInt(WeiPerGwei))
	return gweiDecimal.StringFixed(2)
}
