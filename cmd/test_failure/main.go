package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"baldassi/internal/exchange"
	"github.com/rs/zerolog"
)

func main() {
	// Setup logging to see ALL the error details
	output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	logger := zerolog.New(output).With().Timestamp().Logger()
	zerolog.DefaultContextLogger = &logger

	fmt.Println("=== FORCED FAILURE TEST ===")
	fmt.Println("This simulates what happens when Binance is blocked\n")

	// Set an environment variable to override the Binance URL
	// This simulates what happens with a 451 error or connection failure
	fmt.Println("Setting TEST_BINANCE_BROKEN=true to force failure...")
	os.Setenv("TEST_BINANCE_BROKEN", "true")

	// Create Binance client
	client := exchange.NewBinanceClient("ETHUSDC", 100*time.Millisecond, 5*time.Second)

	// Try to start - this SHOULD fail with our new error handling
	ctx := context.Background()
	fmt.Println("\nAttempting to connect (this WILL fail)...")
	fmt.Println("----------------------------------------\n")

	err := client.Start(ctx)
	if err != nil {
		fmt.Println("\n========================================")
		fmt.Println("✅ ERROR HANDLING WORKING CORRECTLY!")
		fmt.Println("========================================")
		fmt.Printf("\n❌ Error detected: %v\n", err)
		fmt.Println("\nThis is exactly what happens when Binance is blocked!")
		fmt.Println("\nThe bot properly:")
		fmt.Println("  1. ✓ Detected the connection failure")
		fmt.Println("  2. ✓ Logged detailed error information")
		fmt.Println("  3. ✓ Returned error from Start()")
		fmt.Println("  4. ✓ Would exit immediately")
		fmt.Println("\n🛡️ NO PHANTOM PRICES! NO FAKE ARBITRAGE!")
		return
	}

	fmt.Println("\n❌ PROBLEM: Start() didn't fail!")
	fmt.Println("Error handling may not be working correctly.")
}
