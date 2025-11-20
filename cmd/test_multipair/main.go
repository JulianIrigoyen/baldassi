package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"baldassi/internal/config"
	"baldassi/internal/ethereum"
	"baldassi/internal/exchange"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
)

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️  No .env file found")
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("❌ Failed to load config: %v\n", err)
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🧪 MULTI-PAIR INTEGRATION TEST")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("📡 Starting Binance connection...")

	// Get trading pairs from config
	pairs := config.GetTradingPairs()
	fmt.Printf("📊 Testing %d pairs: ", len(pairs))
	for i, p := range pairs {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Print(p.Name)
	}
	fmt.Println()
	fmt.Println()

	testAmount := decimal.NewFromInt(1)
	successCount := 0
	failCount := 0

	for _, pair := range pairs {
		fmt.Printf("[%s] Creating clients...\n", pair.Name)

		// Create Binance client for this pair
		binanceClient := exchange.NewBinanceClient(pair.BinanceSymbol, 100*time.Millisecond, 5*time.Second)
		if err := binanceClient.Start(ctx); err != nil {
			fmt.Printf("[%s] ❌ Binance failed: %v\n", pair.Name, err)
			failCount++
			continue
		}

		// Create RPC client for this pair
		rpcEndpoints := append([]string{cfg.Ethereum.RPCURL}, cfg.Ethereum.FallbackRPCs...)
		ethClient, err := ethereum.NewRpcManager(rpcEndpoints)
		if err != nil {
			fmt.Printf("[%s] ❌ Failed to create RPC client: %v\n", pair.Name, err)
			binanceClient.Stop()
			failCount++
			continue
		}

		uniswapClient, err := exchange.NewUniswapClientForPair(
			ethClient,
			pair,
			100*time.Millisecond,
			1*time.Second,
		)
		if err != nil {
			fmt.Printf("[%s] ❌ Failed to create Uniswap client: %v\n", pair.Name, err)
			binanceClient.Stop()
			failCount++
			continue
		}

		if err := uniswapClient.Start(ctx); err != nil {
			fmt.Printf("[%s] ❌ Failed to start Uniswap: %v\n", pair.Name, err)
			binanceClient.Stop()
			failCount++
			continue
		}

		// Test Binance
		binanceQuote, err := binanceClient.GetPrice(ctx, testAmount)
		if err != nil {
			fmt.Printf("[%s] ❌ Binance error: %v\n", pair.Name, err)
			binanceClient.Stop()
			uniswapClient.Stop()
			failCount++
			continue
		}

		// Test Uniswap
		uniswapQuote, err := uniswapClient.GetPrice(ctx, testAmount)
		if err != nil {
			fmt.Printf("[%s] ❌ Uniswap error: %v\n", pair.Name, err)
			binanceClient.Stop()
			uniswapClient.Stop()
			failCount++
			continue
		}

		binanceClient.Stop()
		uniswapClient.Stop()

		fmt.Printf("[%s] ✅ Success\n", pair.Name)
		fmt.Printf("      Binance: bid=$%.2f ask=$%.2f\n",
			binanceQuote.SellPrice.InexactFloat64(),
			binanceQuote.BuyPrice.InexactFloat64())
		fmt.Printf("      Uniswap: bid=$%.2f ask=$%.2f\n",
			uniswapQuote.SellPrice.InexactFloat64(),
			uniswapQuote.BuyPrice.InexactFloat64())

		spread := binanceQuote.BuyPrice.Sub(uniswapQuote.SellPrice).Abs()
		fmt.Printf("      Spread: $%.2f\n", spread.InexactFloat64())
		fmt.Println()

		successCount++
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 RESULTS")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Total pairs: %d\n", len(pairs))
	fmt.Printf("Success: %d\n", successCount)
	fmt.Printf("Failed: %d\n", failCount)
	fmt.Println()

	if successCount == len(pairs) {
		fmt.Println("✅ All pairs working!")
	} else {
		fmt.Println("⚠️  Some pairs failed")
	}
}
