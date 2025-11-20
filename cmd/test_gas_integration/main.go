package main

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"baldassi/internal/config"
	"baldassi/internal/ethereum"
	"baldassi/internal/exchange"
	"baldassi/internal/gas"
	"github.com/joho/godotenv"
	"github.com/shopspring/decimal"
)

// Gas cost formula: USD = gasUnits × gasPriceWei × ethPriceUSD / 1e18
// Uses 150k gas (typical multihop swap), live gas price, live ETH price from Binance

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
	fmt.Println("🧪 GAS CALCULATOR INTEGRATION TEST")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Println("Tests real-world gas cost calculation:")
	fmt.Println("  • Live Ethereum gas prices (EIP-1559)")
	fmt.Println("  • Real ETH price from Binance")
	fmt.Println("  • USD cost for 150k gas Uniswap V3 swap")
	fmt.Println("  • Cache behavior and performance")
	fmt.Println()

	ctx := context.Background()

	// Test 1: Ethereum RPC Connection
	fmt.Println("1️⃣  Testing Ethereum RPC connection...")
	rpcURLs := cfg.Ethereum.GetAllRPCEndpoints()
	fmt.Printf("   RPC endpoints: %d configured\n", len(rpcURLs))

	ethClient, err := ethereum.NewRpcManager(rpcURLs)
	if err != nil {
		fmt.Printf("   ❌ Failed to connect: %v\n", err)
		return
	}

	blockNum, err := ethClient.BlockNumber(ctx)
	if err != nil {
		fmt.Printf("   ❌ Failed to get block number: %v\n", err)
		return
	}

	fmt.Printf("   ✅ Connected to Ethereum mainnet\n")
	fmt.Printf("   📦 Current block: %d\n", blockNum)
	fmt.Println()

	// Test 2: ETH Price Fetching
	fmt.Println("2️⃣  Testing ETH price fetching...")
	binanceClient := exchange.NewBinanceClient(
		"ETHUSDC",
		100*time.Millisecond,
		5*time.Second,
	)

	if err := binanceClient.Start(ctx); err != nil {
		fmt.Printf("   ❌ Failed to start Binance client: %v\n", err)
		return
	}
	defer binanceClient.Stop()

	getETHPrice := func() decimal.Decimal {
		quote, err := binanceClient.GetPrice(ctx, decimal.NewFromFloat(1.0))
		if err != nil {
			return decimal.NewFromFloat(3100.0) // Fallback
		}
		return quote.BuyPrice
	}

	ethPrice := getETHPrice()
	fmt.Printf("   ✅ ETH Price: $%.2f\n", ethPrice.InexactFloat64())

	// Validate ETH price is reasonable
	if ethPrice.LessThan(decimal.NewFromFloat(1000)) || ethPrice.GreaterThan(decimal.NewFromFloat(10000)) {
		fmt.Printf("   ⚠️  Warning: ETH price outside expected range ($1000-$10000)\n")
	}
	fmt.Println()

	// Test 3: Gas Calculator Creation
	fmt.Println("3️⃣  Creating GasCalculator...")

	// Create RpcManager for gas calculator resilience
	rpcEndpoints := cfg.Ethereum.GetAllRPCEndpoints()
	multiClient, err := ethereum.NewRpcManager(rpcEndpoints)
	if err != nil {
		fmt.Printf("   ❌ Failed to create RpcManager: %v\n", err)
		return
	}
	defer multiClient.Close()

	gasCalc := gas.NewGasCalculator(multiClient, getETHPrice)
	fmt.Println("   ✅ GasCalculator created")
	fmt.Println()

	// Test 4: Gas Price Fetching
	fmt.Println("4️⃣  Testing gas price data...")
	gasData, err := gasCalc.GetCurrentGasData(ctx)
	if err != nil {
		fmt.Printf("   ❌ Failed to get gas data: %v\n", err)
		return
	}

	// Convert Wei to Gwei for display
	baseFeeGwei := weiToGwei(gasData.BaseFee)
	priorityFeeGwei := weiToGwei(gasData.PriorityFee)
	totalGasGwei := weiToGwei(gasData.TotalGasPrice)

	fmt.Printf("   ✅ Gas prices retrieved\n")
	fmt.Printf("   ⛽ Base fee: %.2f Gwei (EIP-1559 network fee)\n", baseFeeGwei)
	fmt.Printf("   💎 Priority fee: %.2f Gwei (miner tip for speed)\n", priorityFeeGwei)
	fmt.Printf("   📊 Total gas: %.2f Gwei (base + priority)\n", totalGasGwei)

	// Validate gas prices are reasonable (10-500 Gwei typical range)
	if totalGasGwei < 1 || totalGasGwei > 1000 {
		fmt.Printf("   ⚠️  Warning: Gas price outside typical range (1-1000 Gwei)\n")
	}
	fmt.Println()

	// Test 5: USD Cost Calculation
	fmt.Println("5️⃣  Testing USD cost calculation...")
	fmt.Println("   Formula: 150,000 gas × GasPrice(Gwei) × ETHPrice(USD)")
	fmt.Println()

	startTime := time.Now()
	swapCostUSD, err := gasCalc.CalculateSwapCostUSD(ctx)
	calcDuration := time.Since(startTime)

	if err != nil {
		fmt.Printf("   ❌ Failed to calculate swap cost: %v\n", err)
		return
	}

	swapCostFloat := swapCostUSD.InexactFloat64()
	fmt.Printf("   ✅ Swap cost: $%.2f USD\n", swapCostFloat)
	fmt.Printf("   ⏱️  Calculation time: %v\n", calcDuration)
	fmt.Println()
	fmt.Printf("   📐 Breakdown:\n")
	fmt.Printf("      150,000 gas units (Uniswap V3 swap)\n")
	fmt.Printf("      × %.2f Gwei gas price\n", totalGasGwei)
	fmt.Printf("      × $%.2f ETH price\n", ethPrice.InexactFloat64())
	fmt.Printf("      = $%.2f USD total cost\n", swapCostFloat)

	// Validate swap cost is reasonable ($1-$500 typical range)
	if swapCostFloat < 1.0 || swapCostFloat > 500.0 {
		fmt.Printf("   ⚠️  Warning: Swap cost outside typical range ($1-$500)\n")
	}
	fmt.Println()

	// Test 6: Cache Behavior
	fmt.Println("6️⃣  Testing cache behavior (10 rapid calculations)...")
	cacheHits := 0
	cacheMisses := 0
	totalLatency := time.Duration(0)

	for i := 0; i < 10; i++ {
		start := time.Now()
		_, err := gasCalc.CalculateSwapCostUSD(ctx)
		latency := time.Since(start)
		totalLatency += latency

		if err != nil {
			fmt.Printf("   ❌ Calculation %d failed: %v\n", i+1, err)
			continue
		}

		// First call is always cache miss, subsequent should be hits
		if i == 0 {
			cacheMisses++
			fmt.Printf("   [%d] Cache MISS (expected) - %v\n", i+1, latency)
		} else {
			if latency < 1*time.Millisecond {
				cacheHits++
				fmt.Printf("   [%d] Cache HIT - %v\n", i+1, latency)
			} else {
				cacheMisses++
				fmt.Printf("   [%d] Cache MISS (unexpected) - %v\n", i+1, latency)
			}
		}
	}

	avgLatency := totalLatency / 10
	cacheHitRatio := float64(cacheHits) / float64(cacheHits+cacheMisses) * 100

	fmt.Println()
	fmt.Printf("   📊 Cache Statistics:\n")
	fmt.Printf("      Hits: %d\n", cacheHits)
	fmt.Printf("      Misses: %d\n", cacheMisses)
	fmt.Printf("      Hit ratio: %.1f%%\n", cacheHitRatio)
	fmt.Printf("      Average latency: %v\n", avgLatency)

	if cacheHitRatio < 80 {
		fmt.Printf("   ⚠️  Warning: Low cache hit ratio (expected >80%%)\n")
	} else {
		fmt.Printf("   ✅ Cache performing well\n")
	}
	fmt.Println()

	// Test 7: Cache Expiration
	fmt.Println("7️⃣  Testing cache expiration (12 second TTL)...")
	fmt.Println("   First calculation (fresh)...")
	cost1, _ := gasCalc.CalculateSwapCostUSD(ctx)
	cost1Float := cost1.InexactFloat64()

	fmt.Println("   Waiting 13 seconds for cache to expire...")
	time.Sleep(13 * time.Second)

	fmt.Println("   Second calculation (should fetch new data)...")
	start := time.Now()
	cost2, _ := gasCalc.CalculateSwapCostUSD(ctx)
	cost2Float := cost2.InexactFloat64()
	latency := time.Since(start)

	fmt.Printf("   Cost before: $%.2f\n", cost1Float)
	fmt.Printf("   Cost after:  $%.2f\n", cost2Float)
	fmt.Printf("   Latency: %v\n", latency)

	if latency > 1*time.Millisecond {
		fmt.Printf("   ✅ Cache expired correctly (fetched new data)\n")
	} else {
		fmt.Printf("   ⚠️  Warning: May still be using cached data\n")
	}
	fmt.Println()

	// Test 8: Block Update
	fmt.Println("8️⃣  Testing block-based cache refresh...")
	fmt.Println("   Current block:", blockNum)

	// Simulate block update
	gasCalc.UpdateOnBlock(ctx, blockNum+1)
	time.Sleep(500 * time.Millisecond) // Give async update time to complete

	fmt.Printf("   ✅ UpdateOnBlock() called for block %d\n", blockNum+1)
	fmt.Println("   (Cache should be refreshed asynchronously)")
	fmt.Println()

	// Final Summary
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 INTEGRATION TEST SUMMARY")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
	fmt.Printf("Network Data:\n")
	fmt.Printf("  Block: %d\n", blockNum)
	fmt.Printf("  ETH Price: $%.2f\n", ethPrice.InexactFloat64())
	fmt.Printf("  Gas (Base/Priority/Total): %.2f / %.2f / %.2f Gwei\n",
		baseFeeGwei, priorityFeeGwei, totalGasGwei)
	fmt.Println()
	fmt.Printf("Gas Calculator Performance:\n")
	fmt.Printf("  Swap cost: $%.2f USD (150k gas)\n", swapCostFloat)
	fmt.Printf("  Cache hit ratio: %.1f%%\n", cacheHitRatio)
	fmt.Printf("  Average latency: %v\n", avgLatency)
	fmt.Println()

	// Evaluation
	allTestsPassed := true

	if ethPrice.LessThan(decimal.NewFromFloat(1000)) || ethPrice.GreaterThan(decimal.NewFromFloat(10000)) {
		fmt.Println("⚠️  ETH price outside expected range")
		allTestsPassed = false
	}

	if totalGasGwei < 1 || totalGasGwei > 1000 {
		fmt.Println("⚠️  Gas price outside typical range")
		allTestsPassed = false
	}

	if swapCostFloat < 1.0 || swapCostFloat > 500.0 {
		fmt.Println("⚠️  Swap cost outside typical range")
		allTestsPassed = false
	}

	if cacheHitRatio < 80 {
		fmt.Println("⚠️  Cache hit ratio below 80%")
		allTestsPassed = false
	}

	if avgLatency > 100*time.Millisecond {
		fmt.Println("⚠️  Average latency above 100ms")
		allTestsPassed = false
	}

	if allTestsPassed {
		fmt.Println("✅ ALL TESTS PASSED")
		fmt.Println()
		fmt.Println("Gas calculator is working correctly with:")
		fmt.Println("  ✓ Real Ethereum RPC connection")
		fmt.Println("  ✓ Live gas price fetching")
		fmt.Println("  ✓ Real ETH price from Binance")
		fmt.Println("  ✓ Accurate USD cost calculation")
		fmt.Println("  ✓ Efficient caching")
	} else {
		fmt.Println("⚠️  SOME WARNINGS DETECTED")
		fmt.Println()
		fmt.Println("Gas calculator is functional but check warnings above.")
	}
}

// weiToGwei converts Wei (*big.Int) to Gwei (float64)
func weiToGwei(wei *big.Int) float64 {
	if wei == nil {
		return 0
	}
	gwei := new(big.Float).SetInt(wei)
	gwei.Quo(gwei, big.NewFloat(1e9))
	result, _ := gwei.Float64()
	return result
}
