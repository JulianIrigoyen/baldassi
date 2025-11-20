package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"baldassi/internal/exchange"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
)

func main() {
	// Setup logging
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	logger := zerolog.New(output).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &logger

	fmt.Println("=== Binance Error Handling Test ===")
	fmt.Println("Testing with VPN potentially blocking Binance...\n")

	ctx := context.Background()

	// Create Binance client
	fmt.Println("1. Creating Binance client...")
	binanceClient := exchange.NewBinanceClient(
		"ETHUSDC",
		100*time.Millisecond,
		5*time.Second,
	)

	// Try to start - this should fail if Binance is blocked
	fmt.Println("2. Attempting to connect to Binance API...")
	if err := binanceClient.Start(ctx); err != nil {
		fmt.Printf("\n❌ EXPECTED FAILURE DETECTED:\n")
		fmt.Printf("   Error: %v\n", err)
		fmt.Println("\n✅ Error handling is working correctly!")
		fmt.Println("   - Start() method properly detected the failure")
		fmt.Println("   - Error was propagated correctly")
		fmt.Println("   - Bot would stop here in production")
		return
	}

	fmt.Println("\n✅ Binance connected successfully!")
	fmt.Println("   (You're not on a Brazil VPN or Binance is accessible)\n")

	// Try to get price
	fmt.Println("3. Testing price fetch...")
	amounts := []float64{1.0, 10.0, 100.0}

	for _, amt := range amounts {
		amount := decimal.NewFromFloat(amt)
		fmt.Printf("\n   Fetching price for %.0f ETH...\n", amt)

		quote, err := binanceClient.GetPrice(ctx, amount)
		if err != nil {
			fmt.Printf("   ❌ Price fetch failed: %v\n", err)
			continue
		}

		fmt.Printf("   ✅ Buy price (ASK): $%.2f per ETH\n", quote.BuyPrice.InexactFloat64())
		fmt.Printf("      Sell price (BID): $%.2f per ETH\n", quote.SellPrice.InexactFloat64())
		fmt.Printf("      Spread: $%.2f\n", quote.BuyPrice.Sub(quote.SellPrice).InexactFloat64())
		fmt.Printf("      Buy total: $%.2f\n", quote.BuyTotal.InexactFloat64())
		fmt.Printf("      Sell total: $%.2f\n", quote.SellTotal.InexactFloat64())
	}

	// Test cache behavior
	fmt.Println("\n4. Testing cache behavior...")
	fmt.Println("   Fetching price again (should hit cache)...")

	start := time.Now()
	_, err := binanceClient.GetPrice(ctx, decimal.NewFromFloat(1.0))
	duration := time.Since(start)

	if err != nil {
		fmt.Printf("   ❌ Cache test failed: %v\n", err)
	} else {
		fmt.Printf("   ✅ Price fetched in %v", duration)
		if duration < 10*time.Millisecond {
			fmt.Println(" (cache hit)")
		} else {
			fmt.Println(" (cache miss or API call)")
		}
	}

	// Clean shutdown
	fmt.Println("\n5. Shutting down...")
	if err := binanceClient.Stop(); err != nil {
		log.Fatal("Failed to stop:", err)
	}

	fmt.Println("\n=== Test Complete ===")
}
