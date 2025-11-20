package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	// WebSocket timeouts
	wsWriteTimeout = 10 * time.Second
	wsReadTimeout  = 60 * time.Second
	wsPingInterval = 30 * time.Second

	// Reconnection settings
	maxReconnectDelay      = 5 * time.Minute
	initialReconnectDelay  = 1 * time.Second
	reconnectBackoffFactor = 2
)

// BlockHeader represents a new block header from eth_subscribe
type BlockHeader struct {
	Number        string `json:"number"`        // Hex encoded block number
	Hash          string `json:"hash"`
	ParentHash    string `json:"parentHash"`
	Timestamp     string `json:"timestamp"` // Hex encoded timestamp
	GasLimit      string `json:"gasLimit"`
	GasUsed       string `json:"gasUsed"`
	BaseFeePerGas string `json:"baseFeePerGas"`
}

// WSMessage represents a WebSocket message from the Ethereum node
type WSMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	ID      int             `json:"id,omitempty"`
	Error   *WSError        `json:"error,omitempty"`
}

// WSError represents an error in a WebSocket response
type WSError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SubscriptionMessage represents a subscription notification
type SubscriptionMessage struct {
	Subscription string      `json:"subscription"`
	Result       BlockHeader `json:"result"`
}

// BlockMonitor monitors new Ethereum blocks via WebSocket
type BlockMonitor struct {
	wsURLs         []string // Multiple WebSocket URLs for fallback
	currentURLIdx  int      // Index of currently active URL
	conn           *websocket.Conn
	subscriptionID string

	blockChan chan BlockHeader
	errorChan chan error

	lastBlock      uint64
	reconnectDelay time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex

	isConnected  bool
	reconnecting bool
}

// NewBlockMonitor creates a new block monitor
func NewBlockMonitor(wsURL string) *BlockMonitor {
	return &BlockMonitor{
		wsURLs:         []string{wsURL},
		currentURLIdx:  0,
		blockChan:      make(chan BlockHeader, 10),
		errorChan:      make(chan error, 10),
		reconnectDelay: initialReconnectDelay,
	}
}

// NewBlockMonitorWithFallbacks creates a new block monitor with fallback URLs
func NewBlockMonitorWithFallbacks(wsURLs []string) *BlockMonitor {
	if len(wsURLs) == 0 {
		wsURLs = []string{"ws://localhost:8546"}
	}
	return &BlockMonitor{
		wsURLs:         wsURLs,
		currentURLIdx:  0,
		blockChan:      make(chan BlockHeader, 10),
		errorChan:      make(chan error, 10),
		reconnectDelay: initialReconnectDelay,
	}
}

// Start begins monitoring blocks
func (m *BlockMonitor) Start(ctx context.Context) error {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Initial connection
	if err := m.connect(); err != nil {
		return fmt.Errorf("initial connection failed: %w", err)
	}

	// Start monitoring goroutines
	m.wg.Add(3)
	go m.readLoop()
	go m.pingLoop()
	go m.reconnectLoop()

	return nil
}

// Stop stops monitoring blocks
func (m *BlockMonitor) Stop() error {
	if m.cancel != nil {
		m.cancel()
	}

	// Close connection
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
	}
	m.mu.Unlock()

	// Wait for goroutines to finish
	m.wg.Wait()

	// Close channels
	close(m.blockChan)
	close(m.errorChan)

	return nil
}

// BlockChan returns the channel for receiving new blocks
func (m *BlockMonitor) BlockChan() <-chan BlockHeader {
	return m.blockChan
}

// ErrorChan returns the channel for receiving errors
func (m *BlockMonitor) ErrorChan() <-chan error {
	return m.errorChan
}

// IsConnected returns whether the monitor is currently connected
func (m *BlockMonitor) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isConnected
}

// connect establishes a WebSocket connection and subscribes to new blocks
func (m *BlockMonitor) connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close existing connection if any
	if m.conn != nil {
		m.conn.Close()
	}

	// Try each WebSocket URL in order until one works
	var lastErr error
	startIdx := m.currentURLIdx

	for i := 0; i < len(m.wsURLs); i++ {
		idx := (startIdx + i) % len(m.wsURLs)
		wsURL := m.wsURLs[idx]

		log.Debug().Str("url", wsURL).Int("attempt", i+1).Int("total", len(m.wsURLs)).Msg("Attempting WebSocket connection")

		// Dial WebSocket
		dialer := websocket.Dialer{
			HandshakeTimeout: 10 * time.Second,
		}

		conn, _, err := dialer.DialContext(m.ctx, wsURL, nil)
		if err != nil {
			log.Warn().Err(err).Str("url", wsURL).Msg("WebSocket connection failed")
			lastErr = err
			continue
		}

		// Subscribe to new blocks
		subscribeMsg := WSMessage{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "eth_subscribe",
			Params:  json.RawMessage(`["newHeads"]`),
		}

		if err := conn.WriteJSON(subscribeMsg); err != nil {
			log.Warn().Err(err).Str("url", wsURL).Msg("Failed to send subscribe message")
			conn.Close()
			lastErr = err
			continue
		}

		// Read subscription confirmation
		var response WSMessage
		if err := conn.ReadJSON(&response); err != nil {
			log.Warn().Err(err).Str("url", wsURL).Msg("Failed to read subscription response")
			conn.Close()
			lastErr = err
			continue
		}

		if response.Error != nil {
			log.Warn().Str("error", response.Error.Message).Str("url", wsURL).Msg("Subscription error")
			conn.Close()
			lastErr = fmt.Errorf("subscription error: %s", response.Error.Message)
			continue
		}

		subscriptionID, ok := response.Result.(string)
		if !ok {
			log.Warn().Str("url", wsURL).Msg("Unexpected subscription response format")
			conn.Close()
			lastErr = fmt.Errorf("unexpected subscription response format")
			continue
		}

		// Success!
		m.conn = conn
		m.subscriptionID = subscriptionID
		m.currentURLIdx = idx
		m.isConnected = true
		m.reconnectDelay = initialReconnectDelay // Reset delay on successful connection

		if idx != startIdx {
			log.Info().Str("url", wsURL).Int("switched_to", idx).Str("subscription_id", subscriptionID).Msg("Switched to working WebSocket endpoint")
		} else {
			log.Info().Str("subscription_id", subscriptionID).Msg("Connected and subscribed to newHeads")
		}
		return nil
	}

	// All URLs failed
	m.isConnected = false
	return fmt.Errorf("failed to connect to any WebSocket endpoint: %w", lastErr)
}

// readLoop reads messages from the WebSocket
func (m *BlockMonitor) readLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			m.mu.RLock()
			conn := m.conn
			connected := m.isConnected
			m.mu.RUnlock()

			if !connected || conn == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(wsReadTimeout))

			var msg WSMessage
			err := conn.ReadJSON(&msg)
			if err != nil {
				m.mu.Lock()
				m.isConnected = false
				m.mu.Unlock()

				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					m.errorChan <- fmt.Errorf("WebSocket read error: %w", err)
				}
				continue
			}

			// Handle subscription notification
			if msg.Method == "eth_subscription" && msg.Params != nil {
				var subMsg SubscriptionMessage
				if err := json.Unmarshal(msg.Params, &subMsg); err != nil {
					m.errorChan <- fmt.Errorf("failed to parse subscription message: %w", err)
					continue
				}

				// Send block to channel
				select {
				case m.blockChan <- subMsg.Result:
					// Update last block
					if blockNum := parseHexUint64(subMsg.Result.Number); blockNum > 0 {
						m.mu.Lock()
						m.lastBlock = blockNum
						m.mu.Unlock()
					}
				case <-m.ctx.Done():
					return
				default:
					// Channel full, skip block
					log.Warn().Msg("Block channel full, skipping block")
				}
			}
		}
	}
}

// pingLoop sends periodic pings to keep the connection alive
func (m *BlockMonitor) pingLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			conn := m.conn
			connected := m.isConnected
			m.mu.RUnlock()

			if connected && conn != nil {
				conn.SetWriteDeadline(time.Now().Add(wsWriteTimeout))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					m.mu.Lock()
					m.isConnected = false
					m.mu.Unlock()

					m.errorChan <- fmt.Errorf("ping failed: %w", err)
				}
			}
		}
	}
}

// reconnectLoop handles automatic reconnection with exponential backoff
func (m *BlockMonitor) reconnectLoop() {
	defer m.wg.Done()

	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			m.mu.RLock()
			connected := m.isConnected
			reconnecting := m.reconnecting
			m.mu.RUnlock()

			if !connected && !reconnecting {
				m.mu.Lock()
				m.reconnecting = true
				lastBlock := m.lastBlock
				delay := m.reconnectDelay
				m.mu.Unlock()

				log.Info().Dur("delay", delay).Uint64("last_block", lastBlock).Msg("Reconnecting...")
				time.Sleep(delay)

				if err := m.connect(); err != nil {
					log.Warn().Err(err).Msg("Reconnection failed")

					// Exponential backoff
					m.mu.Lock()
					m.reconnectDelay *= time.Duration(reconnectBackoffFactor)
					if m.reconnectDelay > maxReconnectDelay {
						m.reconnectDelay = maxReconnectDelay
					}
					m.reconnecting = false
					m.mu.Unlock()
				} else {
					log.Info().Msg("Reconnected successfully")

					m.mu.Lock()
					m.reconnecting = false
					m.mu.Unlock()
				}
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}
}

// parseHexUint64 parses a hex string to uint64
func parseHexUint64(hex string) uint64 {
	if len(hex) < 3 {
		return 0
	}
	// Remove 0x prefix
	if hex[:2] == "0x" {
		hex = hex[2:]
	}
	var value uint64
	if _, err := fmt.Sscanf(hex, "%x", &value); err != nil {
		return 0
	}
	return value
}