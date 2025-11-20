package cache

import (
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// SharedCache provides L2 caching for cross-component shared data
type SharedCache struct {
	mu sync.RWMutex

	// ETH price cache - shared
	ethPrice      decimal.Decimal
	ethPriceBlock uint64
	ethPriceTime  time.Time

	// Block-scoped data
	currentBlock uint64
	blockTime    time.Time
}

// NewSharedCache creates a new L2 shared cache instance
func NewSharedCache() *SharedCache {
	return &SharedCache{}
}

// UpdateBlock updates the current block number and timestamp
func (c *SharedCache) UpdateBlock(blockNumber uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentBlock = blockNumber
	c.blockTime = time.Now()
}

// GetCurrentBlock returns the current block number
func (c *SharedCache) GetCurrentBlock() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentBlock
}

// SetETHPrice updates the cached ETH price for the current block
func (c *SharedCache) SetETHPrice(price decimal.Decimal, blockNumber uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ethPrice = price
	c.ethPriceBlock = blockNumber
	c.ethPriceTime = time.Now()
}

// GetETHPrice returns the cached ETH price if valid for the current block
func (c *SharedCache) GetETHPrice(blockNumber uint64) (decimal.Decimal, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.ethPriceBlock == blockNumber && !c.ethPrice.IsZero() {
		return c.ethPrice, true
	}

	return decimal.Zero, false
}

// GetETHPriceWithFallback returns cached price or executes fallback function
func (c *SharedCache) GetETHPriceWithFallback(blockNumber uint64, fetchFunc func() (decimal.Decimal, error)) decimal.Decimal {
	// Try cache first
	if price, ok := c.GetETHPrice(blockNumber); ok {
		return price
	}

	// Fetch fresh price
	price, err := fetchFunc()
	if err != nil {
		// Return fallback value on error
		return decimal.NewFromInt(3100)
	}

	// Validate price before caching
	if price.IsZero() || price.IsNegative() {
		// Invalid price, use fallback and don't cache
		return decimal.NewFromInt(3100)
	}

	// Update cache with valid price
	c.SetETHPrice(price, blockNumber)
	return price
}

// IsBlockStale checks if we're still on the same block
func (c *SharedCache) IsBlockStale(blockNumber uint64) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentBlock != blockNumber
}

// BlockScopedCache provides block-scoped caching for component data
type BlockScopedCache struct {
	mu sync.RWMutex

	// Cache data
	data      map[string]interface{}
	blockNum  uint64
	timestamp time.Time
}

// NewBlockScopedCache creates a new block-scoped cache
func NewBlockScopedCache(name string) *BlockScopedCache {
	return &BlockScopedCache{
		data: make(map[string]interface{}),
	}
}

// Get retrieves a value from the cache if valid for the current block
func (c *BlockScopedCache) Get(key string, currentBlock uint64) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.blockNum != currentBlock {
		return nil, false
	}

	if val, ok := c.data[key]; ok {
		return val, true
	}

	return nil, false
}

// Set stores a value in the cache for the current block
func (c *BlockScopedCache) Set(key string, value interface{}, blockNumber uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear cache if block changed
	if c.blockNum != blockNumber {
		c.data = make(map[string]interface{})
		c.blockNum = blockNumber
		c.timestamp = time.Now()
	}

	c.data[key] = value
}

// Clear invalidates the entire cache
func (c *BlockScopedCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data = make(map[string]interface{})
	c.blockNum = 0
	c.timestamp = time.Time{}
}

// UpdateBlock clears cache if block number changed
func (c *BlockScopedCache) UpdateBlock(blockNumber uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.blockNum != blockNumber {
		// Clear cache for new block
		c.data = make(map[string]interface{})
		c.blockNum = blockNumber
		c.timestamp = time.Now()
	}
}
