package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"baldassi/internal/arbitrage"
	"baldassi/internal/cache"
	"baldassi/internal/config"
	"baldassi/internal/exchange"
	"baldassi/internal/gas"
	"baldassi/internal/logger"
	"baldassi/internal/monitor"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

func main() {
	// Initialize logger first
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	logger.Initialize(logger.Config{
		Level:  logger.LogLevel(logLevel),
		Pretty: true,
	})

	// Load config
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	// Log startup information
	log.Info().
		Str("version", "1.0").
		Floats64("trade_sizes", cfg.Arbitrage.TradeSizes).
		Float64("min_profit_usd", cfg.Arbitrage.MinProfitUSD).
		Float64("min_profit_percent", cfg.Arbitrage.MinProfitPercent).
		Msg("CEX-DEX Arbitrage Bot started")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize exchanges
	log.Info().Msg("Initializing exchanges")

	// Binance client
	binanceClient := exchange.NewBinanceClient(
		"ETHUSDC",            // Symbol
		100*time.Millisecond, // Rate limit
		5*time.Second,        // Cache TTL
	)
	if err := binanceClient.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start Binance client")
	}
	defer binanceClient.Stop()

	// Uniswap client with automatic failover to backup RPC endpoints
	uniswapClient, err := exchange.NewUniswapClientWithFallbacks(
		cfg.Ethereum.RPCURL,
		cfg.Ethereum.FallbackRPCs,
		100*time.Millisecond, // Rate limit
		1*time.Second,        // Cache TTL - shorter for DEX
	)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Uniswap client")
	}
	if err := uniswapClient.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start Uniswap client")
	}
	defer uniswapClient.Stop()

	// Create shared cache for L2 layer
	log.Info().Msg("Initializing shared cache")
	sharedCache := cache.NewSharedCache()

	// Create gas calculator
	log.Info().Msg("Initializing gas calculator")
	ethClient, err := ethclient.Dial(cfg.Ethereum.RPCURL)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Ethereum RPC")
	}
	defer ethClient.Close()

	// Function to get ETH price using shared cache
	getETHPrice := func() decimal.Decimal {
		// Use shared cache with block-scoped validity
		blockNum := sharedCache.GetCurrentBlock()
		return sharedCache.GetETHPriceWithFallback(blockNum, func() (decimal.Decimal, error) {
			quote, err := binanceClient.GetPrice(ctx, decimal.NewFromInt(1))
			if err != nil {
				return decimal.NewFromInt(3100), nil // Fallback
			}
			// For gas calculation, use the BUY price (conservative estimate)
			// This is what we'd pay to acquire ETH for gas
			return quote.BuyPrice, nil
		})
	}

	gasCalculator := gas.NewGasCalculator(ethClient, getETHPrice)

	// Create arbitrage detector with worker pool for parallel processing
	// Use 3 workers to process trade sizes concurrently
	detector := arbitrage.NewDetectorWithWorkerPool(binanceClient, uniswapClient, gasCalculator, 3)
	defer detector.Stop() // Ensure worker pool is properly cleaned up

	// Initialize checkpoint manager
	checkpointMgr, err := monitor.NewCheckpointManager("./data")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create checkpoint manager")
	}

	// Load last processed block
	lastBlock, err := checkpointMgr.Load()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load checkpoint, starting fresh")
		lastBlock = 0
	} else if lastBlock > 0 {
		log.Info().Uint64("last_block", lastBlock).Msg("Resuming from checkpoint")
	}

	// Create block monitor
	log.Info().Msg("Connecting to Ethereum WebSocket")
	blockMonitor := monitor.NewBlockMonitor(cfg.Ethereum.WSURL)
	if err := blockMonitor.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start block monitor")
	}
	defer blockMonitor.Stop()

	// Convert trade sizes to decimal
	tradeSizes := make([]decimal.Decimal, len(cfg.Arbitrage.TradeSizes))
	for i, size := range cfg.Arbitrage.TradeSizes {
		tradeSizes[i] = decimal.NewFromFloat(size)
	}

	log.Info().Msg("Bot is running! Monitoring for arbitrage opportunities")

	// Main loop
	opportunityCount := 0
	blockCount := 0

	for {
		select {
		case <-sigChan:
			// Save checkpoint before shutting down
			if lastBlock > 0 {
				if err := checkpointMgr.Save(lastBlock); err != nil {
					log.Error().Err(err).Msg("Failed to save final checkpoint")
				} else {
					log.Info().Uint64("last_block", lastBlock).Msg("Saved final checkpoint")
				}
			}

			log.Info().
				Int("blocks_processed", blockCount).
				Int("opportunities_found", opportunityCount).
				Msg("Shutting down gracefully")
			return

		case block := <-blockMonitor.BlockChan():
			blockCount++
			blockNum := parseHexUint64(block.Number)
			lastBlock = blockNum

			blockLogger := log.With().Uint64("block", blockNum).Logger()
			blockLogger.Info().Msg("Processing block")

			// Update shared cache with new block
			sharedCache.UpdateBlock(blockNum)

			// Update Uniswap with new block
			uniswapClient.UpdateBlock(blockNum)

			// Update gas calculator with new block
			gasCalculator.UpdateOnBlock(ctx, blockNum)

			// Save checkpoint periodically
			if blockCount%5 == 0 {
				if err := checkpointMgr.Save(blockNum); err != nil {
					blockLogger.Error().Err(err).Msg("Failed to save checkpoint")
				}
			}

			// Check for arbitrage opportunities
			opportunities, err := detector.CheckArbitrage(ctx, tradeSizes, blockNum)
			if err != nil {
				blockLogger.Error().Err(err).Msg("Failed to check arbitrage")
				continue
			}

			// Debug: Log trading pair prices
			if len(tradeSizes) > 0 {
				testSize := tradeSizes[0]
				binanceQuote, errB := binanceClient.GetPrice(ctx, testSize)
				uniswapQuote, errU := uniswapClient.GetPrice(ctx, testSize)

				if errB == nil && errU == nil {
					blockLogger.Debug().
						Str("pair", "ETH-USDC").
						Float64("size", testSize.InexactFloat64()).
						Float64("binance_bid", binanceQuote.SellPrice.InexactFloat64()).
						Float64("binance_ask", binanceQuote.BuyPrice.InexactFloat64()).
						Float64("uniswap_bid", uniswapQuote.SellPrice.InexactFloat64()).
						Float64("uniswap_ask", uniswapQuote.BuyPrice.InexactFloat64()).
						Msg("Prices")
				}
			}

			// Log any opportunities found
			for _, opp := range opportunities {
				// Check minimum thresholds
				if opp.NetProfit.GreaterThan(decimal.NewFromFloat(cfg.Arbitrage.MinProfitUSD)) &&
					opp.ProfitPercent.GreaterThan(decimal.NewFromFloat(cfg.Arbitrage.MinProfitPercent)) {
					opportunityCount++

					// Log profitable opportunity
					blockLogger.Warn(). // Use Warn level for important events
								Str("pair", opp.Pair).
								Str("direction", opp.Direction).
								Float64("amount", opp.Amount.InexactFloat64()).
								Float64("buy_price", opp.BuyPrice.InexactFloat64()).
								Float64("sell_price", opp.SellPrice.InexactFloat64()).
								Float64("profit_usd", opp.NetProfit.InexactFloat64()).
								Float64("profit_percent", opp.ProfitPercent.InexactFloat64()).
								Float64("gas_cost", opp.GasEstimate.InexactFloat64()).
								Str("buy_exchange", opp.BuyExchange).
								Str("sell_exchange", opp.SellExchange).
								Msg("ARBITRAGE OPPORTUNITY FOUND")
				} else if opp.NetProfit.GreaterThan(decimal.Zero) {
					// Log smaller opportunities at debug level
					blockLogger.Debug().
						Str("pair", opp.Pair).
						Float64("amount", opp.Amount.InexactFloat64()).
						Float64("profit_usd", opp.NetProfit.InexactFloat64()).
						Float64("profit_percent", opp.ProfitPercent.InexactFloat64()).
						Msg("Small opportunity below threshold")
				}
			}

		case err := <-blockMonitor.ErrorChan():
			log.Error().Err(err).Msg("Block monitor error")
			// Exit on critical errors so the bot can be restarted by supervisor
			// The WebSocket reconnection should handle most transient issues
			// but if we're getting errors here, something is seriously wrong
			log.Fatal().Msg("Exiting due to block monitor error")

		case <-time.After(30 * time.Second):
			// Heartbeat - log status every 30 seconds if no blocks
			log.Info().
				Int("blocks_processed", blockCount).
				Int("opportunities_found", opportunityCount).
				Msg("Heartbeat status")
		}
	}
}

// parseHexUint64 converts hex string to uint64
func parseHexUint64(hex string) uint64 {
	if len(hex) < 3 {
		return 0
	}
	if hex[:2] == "0x" {
		hex = hex[2:]
	}
	var value uint64
	if _, err := fmt.Sscanf(hex, "%x", &value); err != nil {
		return 0
	}
	return value
}
