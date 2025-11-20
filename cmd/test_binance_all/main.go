package main

import (
	"context"
	"fmt"
	"time"

	"baldassi/internal/config"
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
	_, err := config.Load()
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🧪 BINANCE COMPREHENSIVE TEST")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	ctx := context.Background()
	testSizes := []decimal.Decimal{
		decimal.NewFromFloat(0.1),
		decimal.NewFromFloat(1.0),
		decimal.NewFromFloat(10.0),
		decimal.NewFromFloat(100.0),
	}

	pairs := config.GetTradingPairs()
	totalSymbols := len(pairs)
	passedSymbols := 0

	for _, pair := range pairs {

		fmt.Printf("═══════════════════════════════════════════════════════════\n")
		fmt.Printf("Testing: %s (Binance: %s)\n", pair.Name, pair.BinanceSymbol)
		fmt.Printf("═══════════════════════════════════════════════════════════\n")
		fmt.Println()

		// Create Binance client
		binanceClient := exchange.NewBinanceClient(
			pair.BinanceSymbol,
			100*time.Millisecond,
			5*time.Second,
		)

		// Test connection
		fmt.Println("1️⃣  Testing connection...")
		if err := binanceClient.Start(ctx); err != nil {
			fmt.Printf("   ❌ Connection failed: %v\n", err)
			fmt.Println()
			continue
		}
		defer binanceClient.Stop()
		fmt.Println("   ✅ Connected successfully")
		fmt.Println()

		// Test orderbook depth
		fmt.Println("2️⃣  Testing orderbook depth...")
		quote, err := binanceClient.GetPrice(ctx, decimal.NewFromFloat(0.1))
		if err != nil {
			fmt.Printf("   ❌ Orderbook fetch failed: %v\n", err)
			fmt.Println()
			continue
		}
		fmt.Printf("   ✅ Orderbook available\n")
		fmt.Printf("   Best Ask: $%.4f\n", quote.BuyPrice.InexactFloat64())
		fmt.Printf("   Best Bid: $%.4f\n", quote.SellPrice.InexactFloat64())
		fmt.Printf("   Spread: $%.4f (%.3f%%)\n",
			quote.BuyPrice.Sub(quote.SellPrice).InexactFloat64(),
			quote.BuyPrice.Sub(quote.SellPrice).Div(quote.SellPrice).Mul(decimal.NewFromInt(100)).InexactFloat64())
		fmt.Println()

		// Test multiple trade sizes
		fmt.Println("3️⃣  Testing slippage across trade sizes...")
		fmt.Printf("   %-10s | %-12s | %-12s | %-10s\n", "Size", "Buy Price", "Sell Price", "Slippage")
		fmt.Printf("   %s\n", "─────────────────────────────────────────────────────────")

		allPassed := true
		for _, size := range testSizes {
			q, err := binanceClient.GetPrice(ctx, size)
			if err != nil {
				fmt.Printf("   %-10s | ❌ Failed: %v\n", size.StringFixed(1), err)
				allPassed = false
				continue
			}

			slippage := q.BuyPrice.Sub(quote.BuyPrice).Div(quote.BuyPrice).Mul(decimal.NewFromInt(100))
			fmt.Printf("   %-10s | $%-11.4f | $%-11.4f | %.4f%%\n",
				size.StringFixed(1),
				q.BuyPrice.InexactFloat64(),
				q.SellPrice.InexactFloat64(),
				slippage.InexactFloat64())
		}
		fmt.Println()

		if !allPassed {
			fmt.Println("   ⚠️  Some trade sizes failed")
		} else {
			fmt.Println("   ✅ All trade sizes working")
			passedSymbols++
		}

		// Test rate limiting
		fmt.Println("4️⃣  Testing rate limiting (10 rapid requests)...")
		startTime := time.Now()
		successCount := 0
		for i := 0; i < 10; i++ {
			_, err := binanceClient.GetPrice(ctx, decimal.NewFromFloat(1.0))
			if err == nil {
				successCount++
			}
		}
		duration := time.Since(startTime)
		fmt.Printf("   ✅ Completed %d/10 requests in %v\n", successCount, duration)
		fmt.Printf("   Average: %v per request\n", duration/10)
		fmt.Println()

		// Get rate limiter metrics
		metrics := binanceClient.GetRateLimiterMetrics()
		fmt.Println("5️⃣  Rate limiter metrics:")
		fmt.Printf("   Total requests: %d\n", metrics.TotalRequests)
		fmt.Printf("   Denied requests: %d\n", metrics.DeniedRequests)
		fmt.Printf("   Current tokens: %d/%d\n", metrics.CurrentTokens, metrics.Capacity)
		fmt.Printf("   Refill rate: %d tokens/sec\n", metrics.RefillRate)
		fmt.Println()
	}

	// Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 BINANCE TEST SUMMARY")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Total symbols tested: %d\n", totalSymbols)
	fmt.Printf("Fully working: %d\n", passedSymbols)
	fmt.Printf("Failed: %d\n", totalSymbols-passedSymbols)
	fmt.Println()

	if passedSymbols == 0 {
		fmt.Println("❌ CRITICAL: Binance API not working for any pair!")
		fmt.Println("   Check:")
		fmt.Println("   - Network connectivity")
		fmt.Println("   - Binance API status (https://www.binance.com/en/support/announcement)")
		fmt.Println("   - Geographic restrictions (Binance may be blocked in your region)")
	} else if passedSymbols < totalSymbols {
		fmt.Printf("⚠️  WARNING: %d/%d symbols failed\n", totalSymbols-passedSymbols, totalSymbols)
		fmt.Println("   Some trading pairs may have issues")
	} else {
		fmt.Println("✅ SUCCESS: All Binance symbols working perfectly!")
	}
}
