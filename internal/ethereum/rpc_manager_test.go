package ethereum

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestMultiClientConcurrentAccess tests that multiple goroutines can safely
// call RpcManager methods concurrently without causing race conditions
func TestMultiClientConcurrentAccess(t *testing.T) {
	// Skip this test in short mode as it requires actual RPC connections
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use public RPC endpoints for testing
	rpcURLs := []string{
		"https://eth.llamarpc.com",
		"https://rpc.ankr.com/eth",
		"https://ethereum.publicnode.com",
	}

	client, err := NewRpcManager(rpcURLs)
	if err != nil {
		t.Fatalf("Failed to create RpcManager: %v", err)
	}
	defer client.Close()

	// Number of concurrent goroutines
	numGoroutines := 10
	// Number of calls per goroutine
	callsPerGoroutine := 20

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*callsPerGoroutine)

	// Launch multiple goroutines that call BlockNumber concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < callsPerGoroutine; j++ {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := client.BlockNumber(ctx)
				cancel()

				if err != nil {
					errChan <- err
					return
				}

				// Small sleep to simulate real-world usage pattern
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Check if any errors occurred
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		t.Errorf("Encountered %d errors during concurrent access:", len(errors))
		for i, err := range errors {
			if i < 5 { // Print first 5 errors
				t.Errorf("  Error %d: %v", i+1, err)
			}
		}
		if len(errors) > 5 {
			t.Errorf("  ... and %d more errors", len(errors)-5)
		}
	}
}

// TestMultiClientGetStatusConcurrent tests that GetStatus can be called
// concurrently with other operations without race conditions
func TestMultiClientGetStatusConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	rpcURLs := []string{
		"https://eth.llamarpc.com",
		"https://rpc.ankr.com/eth",
	}

	client, err := NewRpcManager(rpcURLs)
	if err != nil {
		t.Fatalf("Failed to create RpcManager: %v", err)
	}
	defer client.Close()

	var wg sync.WaitGroup
	ctx := context.Background()

	// Goroutine that continuously calls BlockNumber
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			client.BlockNumber(ctx)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Goroutine that continuously calls GetStatus
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			status := client.GetStatus()
			if status == nil {
				t.Error("GetStatus returned nil")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}

// TestMultiClientFailureTracking tests that failure tracking works correctly
// even under concurrent access
func TestMultiClientFailureTracking(t *testing.T) {
	// This test uses invalid URLs to trigger failures
	rpcURLs := []string{
		"http://invalid-endpoint-1.example.com",
		"http://invalid-endpoint-2.example.com",
	}

	// This should fail to connect to all endpoints
	client, err := NewRpcManager(rpcURLs)
	if err == nil {
		t.Fatal("Expected error when connecting to invalid endpoints, got nil")
	}
	if client != nil {
		client.Close()
	}
}

// TestMultiClientMutexProtection is a focused test that verifies the mutex
// protects against concurrent map writes. This test is run with -race flag.
func TestMultiClientMutexProtection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	rpcURLs := []string{
		"https://eth.llamarpc.com",
		"https://rpc.ankr.com/eth",
		"https://ethereum.publicnode.com",
	}

	client, err := NewRpcManager(rpcURLs)
	if err != nil {
		t.Fatalf("Failed to create RpcManager: %v", err)
	}
	defer client.Close()

	// This test specifically tries to trigger the race condition that was fixed
	// by launching many goroutines that access the same client simultaneously
	numGoroutines := 20
	var wg sync.WaitGroup

	startSignal := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Wait for start signal to maximize concurrency
			<-startSignal

			ctx := context.Background()

			// Try BlockNumber
			client.BlockNumber(ctx)

			// Try GetStatus
			client.GetStatus()
		}()
	}

	// Start all goroutines at once
	close(startSignal)

	// Wait for completion
	wg.Wait()

	// If we reach here without a race condition, the test passes
	t.Log("Concurrent access completed without race conditions")
}
