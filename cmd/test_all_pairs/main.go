package main

import (
	"context"
	"fmt"
	"time"

	"baldassi/internal/config"
	"baldassi/internal/ethereum"
	"baldassi/internal/exchange"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
)

func main() {
	// Load .env
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️  No .env file found")
	} else {
		fmt.Println("✅ Loaded .env file")
	}

	// Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🧪 TRADING PAIRS TEST")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	ctx := context.Background()
	testAmount := decimal.NewFromFloat(1.0) // Test with 1 unit

	pairs := config.GetTradingPairs()
	totalPairs := len(pairs)
	passedPairs := 0

	for i, pair := range pairs {

		fmt.Printf("[%d/%d] Testing %s\n", i+1, totalPairs, pair.Name)
		fmt.Printf("      Base: %s (%s)\n", pair.BaseToken.Symbol, pair.BaseToken.Address[:10]+"...")
		fmt.Printf("      Quote: %s (%s)\n", pair.QuoteToken.Symbol, pair.QuoteToken.Address[:10]+"...")
		fmt.Printf("      Uniswap Pool: %s (fee: %.2f%%)\n", pair.UniswapPool.Address[:10]+"...", float64(pair.UniswapPool.Fee)/10000.0)
		fmt.Printf("      Binance Symbol: %s\n", pair.BinanceSymbol)
		fmt.Println()

		// Test Binance
		fmt.Printf("      📊 Testing Binance (%s)...\n", pair.BinanceSymbol)
		binanceClient := exchange.NewBinanceClient(
			pair.BinanceSymbol,
			100*time.Millisecond,
			5*time.Second,
		)

		if err := binanceClient.Start(ctx); err != nil {
			fmt.Printf("         ❌ Binance connection failed: %v\n", err)
		} else {
			binanceQuote, err := binanceClient.GetPrice(ctx, testAmount)
			if err != nil {
				fmt.Printf("         ❌ Binance price fetch failed: %v\n", err)
			} else {
				fmt.Printf("         ✅ Binance working\n")
				fmt.Printf("            Buy:  $%.2f (ask)\n", binanceQuote.BuyPrice.InexactFloat64())
				fmt.Printf("            Sell: $%.2f (bid)\n", binanceQuote.SellPrice.InexactFloat64())
				fmt.Printf("            Spread: $%.2f (%.3f%%)\n",
					binanceQuote.BuyPrice.Sub(binanceQuote.SellPrice).InexactFloat64(),
					binanceQuote.BuyPrice.Sub(binanceQuote.SellPrice).Div(binanceQuote.SellPrice).Mul(decimal.NewFromInt(100)).InexactFloat64())
			}
			binanceClient.Stop()
		}

		// Test Uniswap
		fmt.Printf("      🦄 Testing Uniswap (pool: %s)...\n", pair.UniswapPool.Address[:10]+"...")

		// Create RPC client for this pair test
		rpcEndpoints := append([]string{cfg.Ethereum.RPCURL}, cfg.Ethereum.FallbackRPCs...)
		ethClient, err := ethereum.NewRpcManager(rpcEndpoints)
		if err != nil {
			fmt.Printf("         ❌ RPC client creation failed: %v\n", err)
			continue
		}

		uniswapClient, err := exchange.NewUniswapClientForPair(
			ethClient,
			pair,
			100*time.Millisecond,
			1*time.Second,
		)

		if err != nil {
			fmt.Printf("         ❌ Uniswap client creation failed: %v\n", err)
		} else {
			if err := uniswapClient.Start(ctx); err != nil {
				fmt.Printf("         ❌ Uniswap connection failed: %v\n", err)
			} else {
				uniswapQuote, err := uniswapClient.GetPrice(ctx, testAmount)
				if err != nil {
					fmt.Printf("         ❌ Uniswap price fetch failed: %v\n", err)
				} else {
					fmt.Printf("         ✅ Uniswap working\n")
					fmt.Printf("            Buy:  $%.2f\n", uniswapQuote.BuyPrice.InexactFloat64())
					fmt.Printf("            Sell: $%.2f\n", uniswapQuote.SellPrice.InexactFloat64())
					fmt.Printf("            Spread: $%.2f (%.3f%%)\n",
						uniswapQuote.BuyPrice.Sub(uniswapQuote.SellPrice).InexactFloat64(),
						uniswapQuote.BuyPrice.Sub(uniswapQuote.SellPrice).Div(uniswapQuote.SellPrice).Mul(decimal.NewFromInt(100)).InexactFloat64())

					// Check for arbitrage opportunity
					if binanceClient != nil {
						binanceQuote, _ := binanceClient.GetPrice(ctx, testAmount)
						if binanceQuote != nil && uniswapQuote != nil {
							// CEX -> DEX (buy on Binance, sell on Uniswap)
							cexToDex := uniswapQuote.SellPrice.Sub(binanceQuote.BuyPrice)
							// DEX -> CEX (buy on Uniswap, sell on Binance)
							dexToCex := binanceQuote.SellPrice.Sub(uniswapQuote.BuyPrice)

							fmt.Printf("      🔍 Arbitrage Check:\n")
							if cexToDex.GreaterThan(decimal.Zero) {
								fmt.Printf("         💰 CEX→DEX: $%.2f profit (%.3f%%)\n",
									cexToDex.InexactFloat64(),
									cexToDex.Div(binanceQuote.BuyPrice).Mul(decimal.NewFromInt(100)).InexactFloat64())
							}
							if dexToCex.GreaterThan(decimal.Zero) {
								fmt.Printf("         💰 DEX→CEX: $%.2f profit (%.3f%%)\n",
									dexToCex.InexactFloat64(),
									dexToCex.Div(uniswapQuote.BuyPrice).Mul(decimal.NewFromInt(100)).InexactFloat64())
							}
							if cexToDex.LessThanOrEqual(decimal.Zero) && dexToCex.LessThanOrEqual(decimal.Zero) {
								fmt.Printf("         ⚖️  No arbitrage opportunity (spreads aligned)\n")
							}
						}
					}

					passedPairs++
				}
				uniswapClient.Stop()
			}
		}

		fmt.Println()
	}

	// Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 PAIRS TEST SUMMARY")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Total pairs tested: %d\n", totalPairs)
	fmt.Printf("Fully working: %d\n", passedPairs)
	fmt.Printf("Failed: %d\n", totalPairs-passedPairs)
	fmt.Println()

	if passedPairs == 0 {
		fmt.Println("❌ CRITICAL: No working trading pairs!")
	} else if passedPairs < totalPairs {
		fmt.Printf("⚠️  WARNING: %d/%d pairs have issues\n", totalPairs-passedPairs, totalPairs)
	} else {
		fmt.Println("✅ SUCCESS: All pairs working!")
	}
}
