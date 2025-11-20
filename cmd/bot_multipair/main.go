package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"baldassi/internal/arbitrage"
	"baldassi/internal/cache"
	"baldassi/internal/config"
	"baldassi/internal/ethereum"
	"baldassi/internal/exchange"
	"baldassi/internal/gas"
	"baldassi/internal/logger"
	"baldassi/internal/monitor"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

type PairResult struct {
	PairName      string
	Opportunities []*exchange.ArbitrageOpportunity
	Error         error
}

func main() {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	logger.Initialize(logger.Config{
		Level:  logger.LogLevel(logLevel),
		Pretty: true,
	})

	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	pairs := config.GetTradingPairs()

	// Create pool address lookup
	poolAddresses := make(map[string]string)
	for _, pair := range pairs {
		poolAddresses[pair.Name] = pair.UniswapPool.Address
	}

	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Str("version", "1.0-multipair").Msg("Multi-Pair CEX-DEX Arbitrage Bot")
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Info().Msg("CONFIGURATION:")
	log.Info().
		Int("trading_pairs", len(pairs)).
		Floats64("trade_sizes", cfg.Arbitrage.TradeSizes).
		Msg("Trading setup")
	log.Info().
		Float64("min_profit_usd", cfg.Arbitrage.MinProfitUSD).
		Float64("min_profit_percent", cfg.Arbitrage.MinProfitPercent).
		Msg("Profit thresholds")
	for _, pair := range pairs {
		log.Info().
			Str("pair", pair.Name).
			Str("binance", pair.BinanceSymbol).
			Str("uniswap_pool", pair.UniswapPool.Address[:10]+"...").
			Msg("Trading pair configured")
	}
	log.Info().Msg("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create shared Ethereum RPC client ONCE for all components
	log.Info().Msg("Connecting to Ethereum RPC endpoints")
	rpcEndpoints := cfg.Ethereum.GetAllRPCEndpoints()
	sharedEthClient, err := ethereum.NewRpcManager(rpcEndpoints)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to Ethereum RPC")
	}
	defer sharedEthClient.Close()

	log.Info().Msg("Initializing exchanges")

	// Create per-pair clients
	pairClients := make(map[string]struct {
		binance *exchange.BinanceClient
		uniswap *exchange.UniswapClient
	})

	for _, pair := range pairs {
		// Create Binance client for this pair
		binanceClient := exchange.NewBinanceClient(
			pair.BinanceSymbol,
			100*time.Millisecond,
			5*time.Second,
		)
		if err := binanceClient.Start(ctx); err != nil {
			log.Error().Str("pair", pair.Name).Err(err).Msg("Failed to start Binance client")
			continue
		}
		defer binanceClient.Stop()

		// Create Uniswap client for this pair (using shared RPC client)
		uniswapClient, err := exchange.NewUniswapClientForPair(
			sharedEthClient,
			pair,
			100*time.Millisecond,
			1*time.Second,
		)
		if err != nil {
			log.Error().Str("pair", pair.Name).Err(err).Msg("Failed to create Uniswap client")
			binanceClient.Stop()
			continue
		}
		if err := uniswapClient.Start(ctx); err != nil {
			log.Error().Str("pair", pair.Name).Err(err).Msg("Failed to start Uniswap client")
			binanceClient.Stop()
			continue
		}
		defer uniswapClient.Stop()

		pairClients[pair.Name] = struct {
			binance *exchange.BinanceClient
			uniswap *exchange.UniswapClient
		}{binanceClient, uniswapClient}

		log.Info().Str("pair", pair.Name).Msg("Initialized pair")
	}

	if len(pairClients) == 0 {
		log.Fatal().Msg("No trading pairs initialized successfully")
	}

	// Shared cache
	log.Info().Msg("Initializing shared cache")
	sharedCache := cache.NewSharedCache()

	// Gas calculator (using shared RPC client)
	log.Info().Msg("Initializing gas calculator")

	getETHPrice := func() decimal.Decimal {
		blockNum := sharedCache.GetCurrentBlock()
		return sharedCache.GetETHPriceWithFallback(blockNum, func() (decimal.Decimal, error) {
			// Use first available Binance client (they all have ETH price access)
			for _, clients := range pairClients {
				quote, err := clients.binance.GetPrice(ctx, decimal.NewFromInt(1))
				if err == nil {
					return quote.BuyPrice, nil
				}
			}
			return decimal.NewFromInt(3100), nil
		})
	}

	gasCalculator := gas.NewGasCalculator(sharedEthClient, getETHPrice)

	// Create per-pair detectors
	detectors := make(map[string]*arbitrage.Detector)
	for pairName, clients := range pairClients {
		detector := arbitrage.NewDetectorWithWorkerPool(clients.binance, clients.uniswap, gasCalculator, 3, pairName, poolAddresses[pairName], decimal.NewFromFloat(cfg.Arbitrage.MinProfitUSD))
		defer detector.Stop()
		detectors[pairName] = detector
	}

	// Checkpoint manager
	checkpointMgr, err := monitor.NewCheckpointManager("./data")
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create checkpoint manager")
	}

	lastBlock, err := checkpointMgr.Load()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load checkpoint, starting fresh")
		lastBlock = 0
	} else if lastBlock > 0 {
		log.Info().Uint64("last_block", lastBlock).Msg("Resuming from checkpoint")
	}

	// Block monitor with fallback support
	log.Info().Msg("Connecting to Ethereum WebSocket")
	wsEndpoints := cfg.Ethereum.GetAllWSEndpoints()
	blockMonitor := monitor.NewBlockMonitorWithFallbacks(wsEndpoints)
	if err := blockMonitor.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to start block monitor")
	}
	defer blockMonitor.Stop()

	// Convert trade sizes
	tradeSizes := make([]decimal.Decimal, len(cfg.Arbitrage.TradeSizes))
	for i, size := range cfg.Arbitrage.TradeSizes {
		tradeSizes[i] = decimal.NewFromFloat(size)
	}

	log.Info().Int("pairs", len(pairClients)).Msg("Bot is running! Monitoring for arbitrage opportunities")

	opportunityCount := 0
	blockCount := 0

	for {
		select {
		case <-sigChan:
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

			// Update caches
			sharedCache.UpdateBlock(blockNum)
			gasCalculator.UpdateOnBlock(ctx, blockNum)

			// Update all Uniswap clients
			for _, clients := range pairClients {
				clients.uniswap.UpdateBlock(blockNum)
			}

			// Process all pairs concurrently
			results := make(chan PairResult, len(detectors))
			var wg sync.WaitGroup

			for pairName, detector := range detectors {
				wg.Add(1)
				go func(name string, det *arbitrage.Detector) {
					defer wg.Done()

					opps, err := det.CheckArbitrage(ctx, tradeSizes, blockNum)
					results <- PairResult{
						PairName:      name,
						Opportunities: opps,
						Error:         err,
					}
				}(pairName, detector)
			}

			// Wait for all pairs
			go func() {
				wg.Wait()
				close(results)
			}()

			// Collect results
			for result := range results {
				if result.Error != nil {
					blockLogger.Error().
						Str("pair", result.PairName).
						Err(result.Error).
						Msg("Failed to check arbitrage")
					continue
				}

				// Print opportunities in challenge format (OR logic: either threshold is sufficient)
				for _, opp := range result.Opportunities {
					if opp.NetProfit.GreaterThan(decimal.NewFromFloat(cfg.Arbitrage.MinProfitUSD)) ||
						opp.ProfitPercent.GreaterThan(decimal.NewFromFloat(cfg.Arbitrage.MinProfitPercent)) {
						opportunityCount++

						// Set pair name and pool address
						if opp.Pair == "" {
							opp.Pair = result.PairName
						}
						if opp.PoolAddress == "" {
							opp.PoolAddress = poolAddresses[result.PairName]
						}

						// Print formatted opportunity matching challenge requirements
						fmt.Print(exchange.FormatArbitrageOpportunity(opp))

						// Also log to JSON for observability
						blockLogger.Warn().
							Str("pair", result.PairName).
							Str("direction", opp.Direction).
							Float64("amount", opp.Amount.InexactFloat64()).
							Float64("net_profit", opp.NetProfit.InexactFloat64()).
							Msg("ARBITRAGE OPPORTUNITY FOUND")
					}
				}
			}

			// Checkpoint
			if blockCount%5 == 0 {
				if err := checkpointMgr.Save(blockNum); err != nil {
					blockLogger.Error().Err(err).Msg("Failed to save checkpoint")
				}
			}

		case err := <-blockMonitor.ErrorChan():
			log.Error().Err(err).Msg("Block monitor error")
			log.Fatal().Msg("Exiting due to block monitor error")

		case <-time.After(30 * time.Second):
			log.Info().
				Int("blocks_processed", blockCount).
				Int("opportunities_found", opportunityCount).
				Msg("Heartbeat status")
		}
	}
}

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
