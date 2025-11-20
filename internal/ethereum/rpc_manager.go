package ethereum

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog/log"
)

// RpcManager manages multiple RPC endpoints with automatic failover
type RpcManager struct {
	urls          []string
	clients       []*ethclient.Client
	current       int
	failures      map[int]int  // Track failures
	permanentFail map[int]bool // Track endpoints that failed during init
	mu            sync.RWMutex // Protects current and failures
}

// NewRpcManager creates a client that tries multiple RPC endpoints
func NewRpcManager(rpcURLs []string) (*RpcManager, error) {
	if len(rpcURLs) == 0 {
		return nil, fmt.Errorf("at least one RPC URL required")
	}

	m := &RpcManager{
		urls:          rpcURLs,
		clients:       make([]*ethclient.Client, len(rpcURLs)),
		failures:      make(map[int]int),
		permanentFail: make(map[int]bool),
	}

	// Try to connect to each endpoint
	var lastErr error
	connectedCount := 0
	firstWorkingIdx := -1
	for i, url := range rpcURLs {
		client, err := ethclient.Dial(url)
		if err != nil {
			log.Warn().Str("url", url).Err(err).Msg("Failed to connect to RPC endpoint")
			m.permanentFail[i] = true // Mark as permanently failed
			lastErr = err
			continue
		}

		// Test connection
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = client.BlockNumber(ctx)
		cancel()

		if err != nil {
			log.Warn().Str("url", url).Err(err).Msg("RPC endpoint not responding")
			client.Close()
			m.permanentFail[i] = true // Mark as permanently failed
			lastErr = err
		} else {
			m.clients[i] = client
			connectedCount++
			if firstWorkingIdx == -1 {
				firstWorkingIdx = i
			}
		}
	}

	// Set current to first working endpoint, not necessarily index 0 if 0 fails
	if firstWorkingIdx != -1 {
		m.current = firstWorkingIdx
	}

	// Log summary of connections
	if connectedCount > 0 {
		log.Info().
			Int("connected", connectedCount).
			Int("total", len(rpcURLs)).
			Str("active", rpcURLs[m.current]).
			Msg("Connected to RPC endpoints")
	}

	// Check if we have at least one working client
	if connectedCount == 0 {
		return nil, fmt.Errorf("no working RPC endpoints available: %w", lastErr)
	}

	return m, nil
}

// CallContract executes eth_call with automatic failover
func (m *RpcManager) CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	m.mu.RLock()
	startIdx := m.current
	m.mu.RUnlock()

	attempts := 0

	for attempts < len(m.urls) {
		idx := (startIdx + attempts) % len(m.urls)
		client := m.clients[idx]

		if client == nil {
			// Try to reconnect to this endpoint
			if err := m.reconnect(idx); err != nil {
				attempts++
				continue
			}
			client = m.clients[idx]
		}

		// Try the call with timeout
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result, err := client.CallContract(callCtx, msg, blockNumber)
		cancel()

		if err == nil {
			// Success! Update current to this working endpoint
			m.mu.Lock()
			oldCurrent := m.current
			if idx != oldCurrent {
				log.Info().
					Int("from", oldCurrent).
					Int("to", idx).
					Str("url", m.urls[idx]).
					Msg("Switched to working RPC endpoint")
			} else {
				//log.Debug().
				//	Int("endpoint", idx).
				//	Str("url", m.urls[idx]).
				//	Msg("Using RPC endpoint")
			}
			m.current = idx
			m.failures[idx] = 0
			m.mu.Unlock()
			return result, nil
		}

		// Log the failure
		m.mu.Lock()
		m.failures[idx]++
		failureCount := m.failures[idx]
		m.mu.Unlock()

		log.Warn().
			Err(err).
			Str("url", m.urls[idx]).
			Int("failures", failureCount).
			Msg("RPC call failed, trying next endpoint")

		// If too many failures, close the connection
		if failureCount >= 3 {
			if client != nil {
				client.Close()
				m.clients[idx] = nil
			}
		}

		attempts++
	}

	return nil, fmt.Errorf("all %d RPC endpoints failed", len(m.urls))
}

// BlockNumber gets the latest block with failover
func (m *RpcManager) BlockNumber(ctx context.Context) (uint64, error) {
	m.mu.RLock()
	startIdx := m.current
	m.mu.RUnlock()

	attempts := 0

	for attempts < len(m.urls) {
		idx := (startIdx + attempts) % len(m.urls)
		client := m.clients[idx]

		if client == nil {
			if err := m.reconnect(idx); err != nil {
				attempts++
				continue
			}
			client = m.clients[idx]
		}

		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		result, err := client.BlockNumber(callCtx)
		cancel()

		if err == nil {
			m.mu.Lock()
			oldCurrent := m.current
			if idx != oldCurrent {
				log.Info().Int("switched_to", idx).Str("url", m.urls[idx]).Msg("Switched to working RPC endpoint")
			} else {
				//log.Debug().Int("endpoint", idx).Str("url", m.urls[idx]).Msg("Using RPC endpoint")
			}
			m.current = idx
			m.failures[idx] = 0
			m.mu.Unlock()
			return result, nil
		}

		m.mu.Lock()
		m.failures[idx]++
		m.mu.Unlock()
		attempts++
	}

	return 0, fmt.Errorf("all RPC endpoints failed")
}

// HeaderByNumber gets block header with failover
func (m *RpcManager) HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error) {
	m.mu.RLock()
	startIdx := m.current
	m.mu.RUnlock()

	attempts := 0

	for attempts < len(m.urls) {
		idx := (startIdx + attempts) % len(m.urls)
		client := m.clients[idx]

		if client == nil {
			if err := m.reconnect(idx); err != nil {
				attempts++
				continue
			}
			client = m.clients[idx]
		}

		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		result, err := client.HeaderByNumber(callCtx, number)
		cancel()

		if err == nil {
			m.mu.Lock()
			oldCurrent := m.current
			if idx != oldCurrent {
				log.Info().Int("switched_to", idx).Str("url", m.urls[idx]).Msg("Switched to working RPC endpoint")
			} else {
				//log.Debug().Int("endpoint", idx).Str("url", m.urls[idx]).Msg("Using RPC endpoint")
			}
			m.current = idx
			m.failures[idx] = 0
			m.mu.Unlock()
			return result, nil
		}

		m.mu.Lock()
		m.failures[idx]++
		m.mu.Unlock()
		attempts++
	}

	return nil, fmt.Errorf("all RPC endpoints failed")
}

// reconnect attempts to reconnect to a specific endpoint
func (m *RpcManager) reconnect(idx int) error {
	if idx >= len(m.urls) {
		return fmt.Errorf("invalid index")
	}

	// Check if endpoint is permanently failed
	m.mu.RLock()
	isPermanentlyFailed := m.permanentFail[idx]
	m.mu.RUnlock()

	if isPermanentlyFailed {
		return fmt.Errorf("endpoint permanently failed during initialization")
	}

	url := m.urls[idx]
	log.Info().Str("url", url).Msg("Attempting to reconnect to RPC endpoint")

	client, err := ethclient.Dial(url)
	if err != nil {
		return err
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = client.BlockNumber(ctx)
	cancel()

	if err != nil {
		client.Close()
		return err
	}

	m.clients[idx] = client
	m.mu.Lock()
	m.failures[idx] = 0
	m.mu.Unlock()
	log.Info().Str("url", url).Msg("Successfully reconnected to RPC endpoint")
	return nil
}

// Close closes all client connections
func (m *RpcManager) Close() {
	for i, client := range m.clients {
		if client != nil {
			client.Close()
			m.clients[i] = nil
		}
	}
}

// SuggestGasPrice gets gas price suggestion with failover
func (m *RpcManager) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	m.mu.RLock()
	startIdx := m.current
	m.mu.RUnlock()

	attempts := 0

	for attempts < len(m.urls) {
		idx := (startIdx + attempts) % len(m.urls)
		client := m.clients[idx]

		if client == nil {
			if err := m.reconnect(idx); err != nil {
				attempts++
				continue
			}
			client = m.clients[idx]
		}

		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		result, err := client.SuggestGasPrice(callCtx)
		cancel()

		if err == nil {
			m.mu.Lock()
			oldCurrent := m.current
			if idx != oldCurrent {
				log.Info().Int("switched_to", idx).Str("url", m.urls[idx]).Msg("Switched to working RPC endpoint")
			}
			m.current = idx
			m.failures[idx] = 0
			m.mu.Unlock()
			return result, nil
		}

		m.mu.Lock()
		m.failures[idx]++
		failureCount := m.failures[idx]
		m.mu.Unlock()

		log.Warn().
			Err(err).
			Str("url", m.urls[idx]).
			Int("failures", failureCount).
			Msg("SuggestGasPrice failed, trying next endpoint")

		if failureCount >= 3 {
			if client != nil {
				client.Close()
				m.clients[idx] = nil
			}
		}

		attempts++
	}

	return nil, fmt.Errorf("all RPC endpoints failed")
}

// SuggestGasTipCap gets gas tip cap suggestion with failover
func (m *RpcManager) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	m.mu.RLock()
	startIdx := m.current
	m.mu.RUnlock()

	attempts := 0

	for attempts < len(m.urls) {
		idx := (startIdx + attempts) % len(m.urls)
		client := m.clients[idx]

		if client == nil {
			if err := m.reconnect(idx); err != nil {
				attempts++
				continue
			}
			client = m.clients[idx]
		}

		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		result, err := client.SuggestGasTipCap(callCtx)
		cancel()

		if err == nil {
			m.mu.Lock()
			oldCurrent := m.current
			if idx != oldCurrent {
				log.Info().Int("switched_to", idx).Str("url", m.urls[idx]).Msg("Switched to working RPC endpoint")
			}
			m.current = idx
			m.failures[idx] = 0
			m.mu.Unlock()
			return result, nil
		}

		m.mu.Lock()
		m.failures[idx]++
		failureCount := m.failures[idx]
		m.mu.Unlock()

		log.Warn().
			Err(err).
			Str("url", m.urls[idx]).
			Int("failures", failureCount).
			Msg("SuggestGasTipCap failed, trying next endpoint")

		if failureCount >= 3 {
			if client != nil {
				client.Close()
				m.clients[idx] = nil
			}
		}

		attempts++
	}

	return nil, fmt.Errorf("all RPC endpoints failed")
}

// GetStatus returns the status of all endpoints
func (m *RpcManager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	status["current"] = m.current
	status["current_url"] = m.urls[m.current]

	endpoints := make([]map[string]interface{}, len(m.urls))
	for i, url := range m.urls {
		endpoints[i] = map[string]interface{}{
			"url":       url,
			"connected": m.clients[i] != nil,
			"failures":  m.failures[i],
		}
	}
	status["endpoints"] = endpoints

	return status
}
