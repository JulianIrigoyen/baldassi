package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"baldassi/internal/logger"
	"baldassi/internal/ratelimit"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
)

const (
	binanceAPIBaseURL        = "https://api.binance.com"
	binanceOrderbookEndpoint = "/api/v3/depth"
	defaultOrderbookLimit    = 100
	binanceTradingFee        = 0.001 // 0.1% trading fee
)

// BinanceOrderbookResponse represents the API response
type BinanceOrderbookResponse struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"` // [price, amount]
	Asks         [][]string `json:"asks"` // [price, amount]
}

// BinanceClient implements the Exchange interface
type BinanceClient struct {
	httpClient *http.Client
	logger     zerolog.Logger
	symbol     string

	// Rate limiting with token bucket
	rateLimiter *ratelimit.TokenBucket

	// Cache
	orderbook     *Orderbook
	orderbookLock sync.RWMutex
	cacheExpiry   time.Time
	cacheTTL      time.Duration
}

// NewBinanceClient creates a new Binance exchange client
func NewBinanceClient(symbol string, rateLimitDelay, cacheTTL time.Duration) *BinanceClient {
	// Create token bucket rate limiter for Binance
	// 20 requests per second with burst capacity of 20
	rateLimiter := ratelimit.NewTokenBucket(
		20,            // capacity (burst)
		20,            // refill rate
		1*time.Second, // refill period
	)

	return &BinanceClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:      logger.WithComponent("binance_" + symbol),
		symbol:      symbol,
		rateLimiter: rateLimiter,
		cacheTTL:    cacheTTL,
	}
}

// Name returns the exchange name
func (b *BinanceClient) Name() string {
	return "Binance"
}

// Type returns the exchange type
func (b *BinanceClient) Type() ExchangeType {
	return TypeCEX
}

// Start init Binance client
func (b *BinanceClient) Start(ctx context.Context) error {
	b.logger.Info().Msg("Attempting to connect to Binance ...")

	// Fetch initial orderbook to verify connectivity
	orderbook, err := b.fetchOrderbook(ctx)
	if err != nil {
		b.logger.Error().
			Err(err).
			Msg("Failed to connect to Binance")
		return fmt.Errorf("binance connectivity test failed: %w", err)
	}

	// Verify we got valid data
	if orderbook == nil || (len(orderbook.Bids) == 0 && len(orderbook.Asks) == 0) {
		b.logger.Error().Msg("Binance returned empty orderbook")
		return fmt.Errorf("binance returned invalid data: empty orderbook")
	}

	b.logger.Info().
		Int("bids", len(orderbook.Bids)).
		Int("asks", len(orderbook.Asks)).
		Msg("Successfully connected to Binance API")
	return nil
}

// Stop shut down gracefully
func (b *BinanceClient) Stop() error {
	b.logger.Info().Msg("Stopped")
	return nil
}

// GetPrice calculates buy and sell prices for a given amount
func (b *BinanceClient) GetPrice(ctx context.Context, amountETH decimal.Decimal) (*PriceQuote, error) {
	orderbook, err := b.fetchOrderbook(ctx)
	if err != nil {
		b.logger.Error().
			Err(err).
			Str("amount_eth", amountETH.String()).
			Msg("Failed to get price from Binance")
		return nil, fmt.Errorf("binance price fetch failed: %w", err)
	}

	// Validate orderbook has both bid and ask data
	if len(orderbook.Asks) == 0 || len(orderbook.Bids) == 0 {
		b.logger.Error().
			Str("amount_eth", amountETH.String()).
			Int("asks", len(orderbook.Asks)).
			Int("bids", len(orderbook.Bids)).
			Msg("Binance orderbook incomplete - need both bids and asks")
		return nil, fmt.Errorf("binance orderbook incomplete: asks=%d bids=%d",
			len(orderbook.Asks), len(orderbook.Bids))
	}

	// Calculate BUY price (from asks) - how much to buy ETH
	buyQuote := b.calculateEffectivePrice(orderbook.Asks, amountETH, true)

	// Calculate SELL price (from bids) - how much we get when selling ETH
	sellQuote := b.calculateEffectivePrice(orderbook.Bids, amountETH, false)

	// Create combined quote with both bid and ask prices
	quote := &PriceQuote{
		// Buy side (ASK)
		BuyPrice:    buyQuote.Price,
		BuyTotal:    buyQuote.TotalCost,
		BuySlippage: buyQuote.Slippage,
		BuyFee:      buyQuote.Fee,

		// Sell side (BID)
		SellPrice:    sellQuote.Price,
		SellTotal:    sellQuote.TotalCost, // This is revenue when selling
		SellSlippage: sellQuote.Slippage,
		SellFee:      sellQuote.Fee,

		Amount:    amountETH,
		Timestamp: orderbook.Timestamp,
	}

	b.logger.Debug().
		Str("amount_eth", amountETH.String()).
		Str("buy_price", quote.BuyPrice.String()).
		Str("sell_price", quote.SellPrice.String()).
		Str("spread", quote.BuyPrice.Sub(quote.SellPrice).String()).
		Msg("Binance bid/ask prices calculated")

	return quote, nil
}

// fetchOrderbook fetches the orderbook from Binance API and caches
func (b *BinanceClient) fetchOrderbook(ctx context.Context) (*Orderbook, error) {
	// Check cache
	b.orderbookLock.RLock()
	if b.orderbook != nil && time.Now().Before(b.cacheExpiry) {
		cached := b.orderbook
		b.orderbookLock.RUnlock()
		return cached, nil
	}
	b.orderbookLock.RUnlock()

	if err := b.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter error: %w", err)
	}

	url := fmt.Sprintf("%s%s?symbol=%s&limit=%d",
		binanceAPIBaseURL,
		binanceOrderbookEndpoint,
		b.symbol,
		defaultOrderbookLimit,
	)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logger.Error().Err(err).Msg("Failed to fetch orderbook from Binance")
		return nil, fmt.Errorf("failed to fetch orderbook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes := make([]byte, 1024)
		n, _ := resp.Body.Read(bodyBytes)
		errorBody := string(bodyBytes[:n])

		b.logger.Error().
			Int("status_code", resp.StatusCode).
			Str("status", resp.Status).
			Str("response_body", errorBody).
			Str("url", url).
			Msg("Binance API request failed")

		//TODO NTH -> Extend / Define generic exchange error handler with known errs to DRY
		switch resp.StatusCode {
		case 451:
			return nil, fmt.Errorf("binance unavailable in your region for legal reasons (HTTP 451 error code)")
		case 429:
			return nil, fmt.Errorf("rate limit exceeded (HTTP 429)")
		case 403:
			return nil, fmt.Errorf("access forbidden - check IP restrictions (HTTP 403)")
		default:
			return nil, fmt.Errorf("API error: %s (HTTP %d)", resp.Status, resp.StatusCode)
		}
	}

	var binanceResp BinanceOrderbookResponse
	if err := json.NewDecoder(resp.Body).Decode(&binanceResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	b.logger.Debug().
		Int("bids_count", len(binanceResp.Bids)).
		Int("asks_count", len(binanceResp.Asks)).
		Str("best_ask", func() string {
			if len(binanceResp.Asks) > 0 {
				return binanceResp.Asks[0][0]
			}
			return "none"
		}()).
		Msg("Fetched orderbook")

	orderbook := &Orderbook{
		LastUpdateID: binanceResp.LastUpdateID,
		Timestamp:    time.Now(),
		Bids:         make([]OrderbookLevel, 0, len(binanceResp.Bids)),
		Asks:         make([]OrderbookLevel, 0, len(binanceResp.Asks)),
	}

	for _, bid := range binanceResp.Bids {
		if len(bid) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(bid[0], 64)
		if err != nil {
			continue
		}
		amount, err := strconv.ParseFloat(bid[1], 64)
		if err != nil {
			continue
		}
		orderbook.Bids = append(orderbook.Bids, OrderbookLevel{
			Price:  decimal.NewFromFloat(price),
			Amount: decimal.NewFromFloat(amount),
		})
	}

	for _, ask := range binanceResp.Asks {
		if len(ask) < 2 {
			continue
		}
		price, err := strconv.ParseFloat(ask[0], 64)
		if err != nil {
			continue
		}
		amount, err := strconv.ParseFloat(ask[1], 64)
		if err != nil {
			continue
		}
		orderbook.Asks = append(orderbook.Asks, OrderbookLevel{
			Price:  decimal.NewFromFloat(price),
			Amount: decimal.NewFromFloat(amount),
		})
	}

	// Update cache
	b.orderbookLock.Lock()
	b.orderbook = orderbook
	b.cacheExpiry = time.Now().Add(b.cacheTTL)
	b.orderbookLock.Unlock()

	return orderbook, nil
}

// OrderbookQuote internal structure for orderbook calculations
type OrderbookQuote struct {
	Price     decimal.Decimal
	TotalCost decimal.Decimal
	Slippage  decimal.Decimal
	Fee       decimal.Decimal
}

// calculateEffectivePrice walks through orderbook levels to calculate effective price
func (b *BinanceClient) calculateEffectivePrice(levels []OrderbookLevel, amountETH decimal.Decimal, isBuy bool) *OrderbookQuote {
	remaining := amountETH
	totalCost := decimal.Zero
	lastPrice := decimal.Zero

	for _, level := range levels {
		if remaining.LessThanOrEqual(decimal.Zero) {
			break
		}

		// How much can we take from this level?
		takeAmount := decimal.Min(remaining, level.Amount)

		// Add to total cost
		levelCost := takeAmount.Mul(level.Price)
		totalCost = totalCost.Add(levelCost)

		// Update remaining and last price
		remaining = remaining.Sub(takeAmount)
		lastPrice = level.Price
	}

	// If we couldn't fill the entire order, use the last available price for remaining
	if remaining.GreaterThan(decimal.Zero) {
		totalCost = totalCost.Add(remaining.Mul(lastPrice))
	}

	// Calculate effective price
	effectivePrice := totalCost.Div(amountETH)

	// Calculate slippage
	bestPrice := decimal.Zero
	if len(levels) > 0 {
		bestPrice = levels[0].Price
	}

	slippage := decimal.Zero
	if bestPrice.GreaterThan(decimal.Zero) {
		slippage = effectivePrice.Sub(bestPrice).Div(bestPrice).Mul(decimal.NewFromInt(100))
	}

	// consider exchange fees
	fee := totalCost.Mul(decimal.NewFromFloat(binanceTradingFee))

	return &OrderbookQuote{
		Price:     effectivePrice,
		TotalCost: totalCost.Add(fee),
		Slippage:  slippage.Abs(),
		Fee:       fee,
	}
}

// GetRateLimiterMetrics returns rate limiter metrics
func (b *BinanceClient) GetRateLimiterMetrics() ratelimit.Metrics {
	return b.rateLimiter.GetMetrics()
}
