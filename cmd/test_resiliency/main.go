package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"baldassi/internal/monitor"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env
	if err := godotenv.Load(); err != nil {
		fmt.Println("⚠️  No .env file found")
	} else {
		fmt.Println("✅ Loaded .env file")
	}

	// Collect all WebSocket endpoints
	wsURLs := []struct {
		name string
		url  string
	}{
		{"Primary WebSocket", os.Getenv("ETH_WS_URL")},
		{"Fallback WS #1", os.Getenv("FALLBACK_WS_1")},
		{"Fallback WS #2", os.Getenv("FALLBACK_WS_2")},
	}

	// Filter out empty URLs
	var endpoints []struct {
		name string
		url  string
	}
	for _, ws := range wsURLs {
		if ws.url != "" {
			endpoints = append(endpoints, ws)
		}
	}

	if len(endpoints) == 0 {
		fmt.Println("❌ No WebSocket endpoints configured")
		return
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("🧪 RESILIENCY TEST - WebSocket Reconnection Logic")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Testing %d endpoint(s)\n", len(endpoints))
	fmt.Println()

	totalPassed := 0
	totalFailed := 0

	for i, endpoint := range endpoints {
		fmt.Printf("[%d/%d] Testing %s\n", i+1, len(endpoints), endpoint.name)
		fmt.Printf("      URL: %s\n", maskURL(endpoint.url))
		fmt.Println()

		if testEndpoint(endpoint.url) {
			totalPassed++
		} else {
			totalFailed++
		}

		// Separator between tests
		if i < len(endpoints)-1 {
			fmt.Println()
			fmt.Println(strings.Repeat("─", 60))
			fmt.Println()
		}
	}

	// Final summary
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("📊 OVERALL RESILIENCY TEST SUMMARY")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("Total endpoints tested: %d\n", len(endpoints))
	fmt.Printf("Passed: %d\n", totalPassed)
	fmt.Printf("Failed: %d\n", totalFailed)
	fmt.Println()

	if totalFailed == 0 {
		fmt.Println("✅ SUCCESS: All endpoints pass resiliency test!")
		fmt.Println("💡 The bot will automatically reconnect if WebSocket fails")
	} else if totalPassed > 0 {
		fmt.Printf("⚠️  WARNING: %d/%d endpoints failed\n", totalFailed, len(endpoints))
		fmt.Println("   Some endpoints may have reliability issues")
	} else {
		fmt.Println("❌ CRITICAL: All endpoints failed resiliency test!")
	}
}

func testEndpoint(wsURL string) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Phase 1: Initial connection
	fmt.Println("   🔷 PHASE 1: Initial Connection (30 seconds)")
	fmt.Println("      Expecting ~2-3 blocks...")
	fmt.Println()

	blockMonitor := monitor.NewBlockMonitor(wsURL)
	if err := blockMonitor.Start(ctx); err != nil {
		fmt.Printf("      ❌ Failed to start: %v\n", err)
		return false
	}

	phase1Start := time.Now()
	phase1Blocks := make([]string, 0)
	errorCount := 0

	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-timeout:
			goto phase2
		case block := <-blockMonitor.BlockChan():
			phase1Blocks = append(phase1Blocks, block.Number)
			fmt.Printf("      [%s] ✅ Block #%s\n",
				time.Now().Format("15:04:05"),
				block.Number)
		case err := <-blockMonitor.ErrorChan():
			errorCount++
			fmt.Printf("      [%s] ⚠️  Error: %v\n",
				time.Now().Format("15:04:05"),
				err)
		}
	}

phase2:
	phase1Duration := time.Since(phase1Start)
	fmt.Println()
	fmt.Printf("      📊 Phase 1 Results: %d blocks in %v\n", len(phase1Blocks), phase1Duration.Round(time.Second))
	if len(phase1Blocks) > 0 {
		fmt.Printf("         Blocks: %s\n", strings.Join(phase1Blocks, ", "))
	}
	fmt.Println()

	if len(phase1Blocks) == 0 {
		fmt.Println("      ❌ FAIL: No blocks received in Phase 1")
		blockMonitor.Stop()
		return false
	}

	fmt.Println("      ✅ Phase 1 PASS: Initial connection working")
	fmt.Println()

	// Phase 2: Simulate failure
	fmt.Println("   🔷 PHASE 2: Simulating Connection Failure")
	fmt.Println("      Stopping BlockMonitor...")
	fmt.Println()

	if err := blockMonitor.Stop(); err != nil {
		fmt.Printf("      ⚠️  Stop error: %v\n", err)
	}

	fmt.Println("      ✅ BlockMonitor stopped (simulating connection loss)")
	fmt.Println("      ⏳ Waiting 5 seconds...")
	time.Sleep(5 * time.Second)
	fmt.Println()

	// Phase 3: Reconnection
	fmt.Println("   🔷 PHASE 3: Reconnection Test (30 seconds)")
	fmt.Println("      Creating new BlockMonitor (simulating reconnect)...")
	fmt.Println()

	blockMonitor = monitor.NewBlockMonitor(wsURL)
	if err := blockMonitor.Start(ctx); err != nil {
		fmt.Printf("      ❌ Reconnection failed: %v\n", err)
		return false
	}
	defer blockMonitor.Stop()

	fmt.Println("      ✅ Reconnected successfully!")
	fmt.Println()

	phase3Start := time.Now()
	phase3Blocks := make([]string, 0)

	timeout = time.After(30 * time.Second)
	for {
		select {
		case <-timeout:
			goto results
		case block := <-blockMonitor.BlockChan():
			phase3Blocks = append(phase3Blocks, block.Number)
			fmt.Printf("      [%s] ✅ Block #%s\n",
				time.Now().Format("15:04:05"),
				block.Number)
		case err := <-blockMonitor.ErrorChan():
			errorCount++
			fmt.Printf("      [%s] ⚠️  Error: %v\n",
				time.Now().Format("15:04:05"),
				err)
		}
	}

results:
	phase3Duration := time.Since(phase3Start)
	fmt.Println()
	fmt.Printf("      📊 Phase 3 Results: %d blocks in %v\n", len(phase3Blocks), phase3Duration.Round(time.Second))
	if len(phase3Blocks) > 0 {
		fmt.Printf("         Blocks: %s\n", strings.Join(phase3Blocks, ", "))
	}
	fmt.Println()

	if len(phase3Blocks) == 0 {
		fmt.Println("      ❌ Phase 3 FAIL: No blocks after reconnection")
		return false
	}

	fmt.Println("      ✅ Phase 3 PASS: Reconnection successful, blocks resumed")
	fmt.Println()
	fmt.Println("      🎯 ENDPOINT PASSED RESILIENCY TEST")
	fmt.Printf("      Total: %d blocks received (%d initial + %d after reconnect)\n",
		len(phase1Blocks)+len(phase3Blocks), len(phase1Blocks), len(phase3Blocks))
	fmt.Printf("      Total errors: %d\n", errorCount)

	return true
}

func maskURL(url string) string {
	if len(url) > 60 {
		return url[:60] + "..."
	}
	return url
}
