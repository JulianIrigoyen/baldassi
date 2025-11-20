package exchange

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"baldassi/internal/cache"
	"baldassi/internal/config"
	myeth "baldassi/internal/ethereum" // Renamed to avoid conflict
	"baldassi/internal/logger"
	"baldassi/internal/ratelimit"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
)

const (
	// Contract addresses
	quoterV2Address = "0x61fFE014bA17989E743c5F6cB21bF9697530B21e"
	wethAddress     = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	usdcAddress     = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"

	// Pool configuration
	poolFee = 3000 // 0.3% fee tier

	// Token decimals
	ethDecimals  = 18
	usdcDecimals = 6

	// Uniswap pool fee
	uniswapFee = 0.003 // 0.3%
)

// QuoterV2 ABI for quote methods
const quoterV2ABI = `[{
    "inputs": [{
        "components": [
            {"internalType": "address", "name": "tokenIn", "type": "address"},
            {"internalType": "address", "name": "tokenOut", "type": "address"},
            {"internalType": "uint256", "name": "amountIn", "type": "uint256"},
            {"internalType": "uint24", "name": "fee", "type": "uint24"},
            {"internalType": "uint160", "name": "sqrtPriceLimitX96", "type": "uint160"}
        ],
        "internalType": "struct IQuoterV2.QuoteExactInputSingleParams",
        "name": "params",
        "type": "tuple"
    }],
    "name": "quoteExactInputSingle",
    "outputs": [
        {"internalType": "uint256", "name": "amountOut", "type": "uint256"},
        {"internalType": "uint160", "name": "sqrtPriceX96After", "type": "uint160"},
        {"internalType": "uint32", "name": "initializedTicksCrossed", "type": "uint32"},
        {"internalType": "uint256", "name": "gasEstimate", "type": "uint256"}
    ],
    "stateMutability": "nonpayable",
    "type": "function"
}, {
    "inputs": [{
        "components": [
            {"internalType": "address", "name": "tokenIn", "type": "address"},
            {"internalType": "address", "name": "tokenOut", "type": "address"},
            {"internalType": "uint256", "name": "amount", "type": "uint256"},
            {"internalType": "uint24", "name": "fee", "type": "uint24"},
            {"internalType": "uint160", "name": "sqrtPriceLimitX96", "type": "uint160"}
        ],
        "internalType": "struct IQuoterV2.QuoteExactOutputSingleParams",
        "name": "params",
        "type": "tuple"
    }],
    "name": "quoteExactOutputSingle",
    "outputs": [
        {"internalType": "uint256", "name": "amountIn", "type": "uint256"},
        {"internalType": "uint160", "name": "sqrtPriceX96After", "type": "uint160"},
        {"internalType": "uint32", "name": "initializedTicksCrossed", "type": "uint32"},
        {"internalType": "uint256", "name": "gasEstimate", "type": "uint256"}
    ],
    "stateMutability": "nonpayable",
    "type": "function"
}]`

// QuoterV2 parameter structs for ABI packing
type IQuoterV2QuoteExactInputSingleParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	AmountIn          *big.Int
	Fee               *big.Int
	SqrtPriceLimitX96 *big.Int
}

type IQuoterV2QuoteExactOutputSingleParams struct {
	TokenIn           common.Address
	TokenOut          common.Address
	Amount            *big.Int
	Fee               *big.Int
	SqrtPriceLimitX96 *big.Int
}

// UniswapClient implements the Exchange interface for Uniswap V3
type UniswapClient struct {
	ethClient    *myeth.RpcManager // Changed to RpcManager for failover
	quoterABI    abi.ABI
	currentBlock uint64
	logger       zerolog.Logger

	// Trading pair configuration
	baseTokenAddr  common.Address // e.g., WETH
	quoteTokenAddr common.Address // e.g., USDC
	poolFee        uint32         // Pool fee tier (e.g., 500, 3000, 10000)
	baseDecimals   int            // Base token decimals
	quoteDecimals  int            // Quote token decimals

	// Block-scoped cache
	blockCache *cache.BlockScopedCache

	// Rate limiting with token bucket
	rateLimiter *ratelimit.TokenBucket
}

// NewUniswapClient creates a new Uniswap V3 client (for backward compatibility - uses ETH-USDC)
func NewUniswapClient(rpcURL string, rateLimitDelay, cacheTTL time.Duration) (*UniswapClient, error) {
	// Call the new function with empty fallbacks and default ETH-USDC pair
	return NewUniswapClientWithFallbacks(rpcURL, nil, rateLimitDelay, cacheTTL)
}

// NewUniswapClientWithFallbacks creates a new Uniswap V3 client with multiple RPC endpoints (uses default ETH-USDC)
func NewUniswapClientWithFallbacks(primaryRPC string, fallbackRPCs []string, rateLimitDelay, cacheTTL time.Duration) (*UniswapClient, error) {
	// Build endpoints list with primary and fallbacks
	rpcEndpoints := []string{primaryRPC}
	rpcEndpoints = append(rpcEndpoints, fallbackRPCs...)

	// Connect to Ethereum nodes with failover
	ethClient, err := myeth.NewRpcManager(rpcEndpoints)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to any Ethereum node: %w", err)
	}

	// Use default ETH-USDC pair for backward compatibility
	defaultPair := config.TradingPair{
		Name: "ETH-USDC",
		BaseToken: config.Token{
			Address:  wethAddress,
			Decimals: ethDecimals,
			Symbol:   "WETH",
		},
		QuoteToken: config.Token{
			Address:  usdcAddress,
			Decimals: usdcDecimals,
			Symbol:   "USDC",
		},
		UniswapPool: config.PoolConfig{
			Fee: poolFee,
		},
	}
	return NewUniswapClientForPair(ethClient, defaultPair, rateLimitDelay, cacheTTL)
}

// NewUniswapClientForPair creates a new Uniswap V3 client for a specific trading pair
func NewUniswapClientForPair(ethClient *myeth.RpcManager, pair config.TradingPair, rateLimitDelay, cacheTTL time.Duration) (*UniswapClient, error) {
	// Use the shared Ethereum RPC client (no connection needed here)

	// Parse QuoterV2 ABI for manual calls
	parsedABI, err := abi.JSON(strings.NewReader(quoterV2ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse QuoterV2 ABI: %w", err)
	}

	// Create token bucket rate limiter for Ethereum RPC
	// 10 requests per second with burst capacity of 30
	rateLimiter := ratelimit.NewTokenBucket(
		30,            // capacity (burst)
		10,            // refill rate
		1*time.Second, // refill period
	)

	return &UniswapClient{
		ethClient:      ethClient,
		quoterABI:      parsedABI,
		logger:         logger.WithComponent("uniswap_" + pair.Name),
		baseTokenAddr:  common.HexToAddress(pair.BaseToken.Address),
		quoteTokenAddr: common.HexToAddress(pair.QuoteToken.Address),
		poolFee:        uint32(pair.UniswapPool.Fee),
		baseDecimals:   pair.BaseToken.Decimals,
		quoteDecimals:  pair.QuoteToken.Decimals,
		blockCache:     cache.NewBlockScopedCache("uniswap_" + pair.Name),
		rateLimiter:    rateLimiter,
	}, nil
}

// Name returns the exchange name
func (u *UniswapClient) Name() string {
	return "Uniswap V3"
}

// Type returns the exchange type
func (u *UniswapClient) Type() ExchangeType {
	return TypeDEX
}

// Start initializes the Uniswap client
func (u *UniswapClient) Start(ctx context.Context) error {
	// Get current block number
	blockNumber, err := u.ethClient.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("failed to get block number: %w", err)
	}
	u.currentBlock = blockNumber

	u.logger.Info().Uint64("block", blockNumber).Msg("Started successfully")
	return nil
}

// Stop cleanly shuts down
func (u *UniswapClient) Stop() error {
	if u.ethClient != nil {
		u.ethClient.Close()
	}
	u.logger.Info().Msg("Stopped")
	return nil
}

// GetPrice gets BOTH buy and sell quotes from Uniswap V3 using QuoterV2
func (u *UniswapClient) GetPrice(ctx context.Context, amountETH decimal.Decimal) (*PriceQuote, error) {
	cacheKey := amountETH.String()

	// Check block-scoped cache first
	if cached, ok := u.blockCache.Get(cacheKey, u.currentBlock); ok {
		if quote, ok := cached.(*PriceQuote); ok {
			return quote, nil
		}
	}

	if err := u.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	amountInWei := u.tokenToWei(amountETH, u.baseDecimals)

	sellParams := IQuoterV2QuoteExactInputSingleParams{
		TokenIn:           u.baseTokenAddr,
		TokenOut:          u.quoteTokenAddr,
		AmountIn:          amountInWei,
		Fee:               big.NewInt(int64(u.poolFee)),
		SqrtPriceLimitX96: big.NewInt(0),
	}

	sellData, err := u.quoterABI.Pack("quoteExactInputSingle", sellParams)
	if err != nil {
		return nil, fmt.Errorf("failed to pack sell call data: %w", err)
	}

	quoterAddr := common.HexToAddress(quoterV2Address)
	sellMsg := ethereum.CallMsg{
		To:   &quoterAddr,
		Data: sellData,
	}

	sellResult, err := u.ethClient.CallContract(ctx, sellMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call QuoterV2 for sell: %w", err)
	}

	sellOutputs, err := u.quoterABI.Unpack("quoteExactInputSingle", sellResult)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack sell result: %w", err)
	}

	if len(sellOutputs) < 4 {
		return nil, fmt.Errorf("unexpected sell output count: got %d, want 4", len(sellOutputs))
	}

	sellAmountOut := sellOutputs[0].(*big.Int)
	if sellAmountOut.Sign() <= 0 {
		return nil, fmt.Errorf("invalid QuoterV2 sell output: got %s", sellAmountOut.String())
	}

	sellQuoteAmount := u.tokenFromWei(sellAmountOut, u.quoteDecimals)
	sellPrice := sellQuoteAmount.Div(amountETH)
	sellFee := sellQuoteAmount.Mul(decimal.NewFromFloat(uniswapFee)).Div(decimal.NewFromFloat(1 - uniswapFee))

	buyParams := IQuoterV2QuoteExactOutputSingleParams{
		TokenIn:           u.quoteTokenAddr,
		TokenOut:          u.baseTokenAddr,
		Amount:            amountInWei,
		Fee:               big.NewInt(int64(u.poolFee)),
		SqrtPriceLimitX96: big.NewInt(0),
	}

	buyData, err := u.quoterABI.Pack("quoteExactOutputSingle", buyParams)
	if err != nil {
		return nil, fmt.Errorf("failed to pack buy call data: %w", err)
	}

	buyMsg := ethereum.CallMsg{
		To:   &quoterAddr,
		Data: buyData,
	}

	buyResult, err := u.ethClient.CallContract(ctx, buyMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call QuoterV2 for buy: %w", err)
	}

	buyOutputs, err := u.quoterABI.Unpack("quoteExactOutputSingle", buyResult)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack buy result: %w", err)
	}

	if len(buyOutputs) < 4 {
		return nil, fmt.Errorf("unexpected buy output count: got %d, want 4", len(buyOutputs))
	}

	buyAmountIn := buyOutputs[0].(*big.Int)
	if buyAmountIn.Sign() <= 0 {
		return nil, fmt.Errorf("invalid QuoterV2 buy output: got %s", buyAmountIn.String())
	}

	buyQuoteAmount := u.tokenFromWei(buyAmountIn, u.quoteDecimals)
	buyPrice := buyQuoteAmount.Div(amountETH)
	buyFee := buyQuoteAmount.Mul(decimal.NewFromFloat(uniswapFee)).Div(decimal.NewFromFloat(1 - uniswapFee))

	blockNumber, _ := u.ethClient.BlockNumber(ctx)

	quote := &PriceQuote{
		BuyPrice:    buyPrice,
		BuyTotal:    buyQuoteAmount.Add(buyFee),
		BuySlippage: decimal.Zero,
		BuyFee:      buyFee,

		SellPrice:    sellPrice,
		SellTotal:    sellQuoteAmount.Sub(sellFee),
		SellSlippage: decimal.Zero,
		SellFee:      sellFee,

		Amount:      amountETH,
		Timestamp:   time.Now(),
		BlockNumber: blockNumber,
	}

	// Store in block-scoped cache
	u.blockCache.Set(cacheKey, quote, u.currentBlock)

	return quote, nil
}

// UpdateBlock updates the current block number
func (u *UniswapClient) UpdateBlock(blockNumber uint64) {
	u.currentBlock = blockNumber
	// Block-scoped cache will automatically invalidate on new block
	u.blockCache.UpdateBlock(blockNumber)
}

// tokenToWei converts token amount to smallest unit (e.g., ETH to Wei)
func (u *UniswapClient) tokenToWei(amount decimal.Decimal, decimals int) *big.Int {
	// Multiply by 10^decimals
	multiplier := decimal.New(1, int32(decimals))
	wei := amount.Mul(multiplier)

	// Convert to big.Int
	weiInt, _ := new(big.Int).SetString(wei.StringFixed(0), 10)
	if weiInt == nil {
		weiInt = big.NewInt(0)
	}
	return weiInt
}

// tokenFromWei converts token from smallest units to decimal
func (u *UniswapClient) tokenFromWei(wei *big.Int, decimals int) decimal.Decimal {
	// Convert to decimal and divide by 10^decimals
	amount := decimal.NewFromBigInt(wei, 0)
	divisor := decimal.New(1, int32(decimals))
	return amount.Div(divisor)
}

// ethToWei converts ETH amount to Wei (18 decimals) - kept for backward compatibility
func (u *UniswapClient) ethToWei(eth decimal.Decimal) *big.Int {
	return u.tokenToWei(eth, ethDecimals)
}

// usdcFromWei converts USDC from smallest units (6 decimals) to decimal - kept for backward compatibility
func (u *UniswapClient) usdcFromWei(wei *big.Int) decimal.Decimal {
	return u.tokenFromWei(wei, usdcDecimals)
}
