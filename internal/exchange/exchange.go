package exchange

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// ExchangeType represents the type of exchange
type ExchangeType string

const (
	TypeCEX ExchangeType = "CEX"
	TypeDEX ExchangeType = "DEX"
)

// PriceQuote represents a price quote for a specific amount
// This includes both BID (sell) and ASK (buy) prices for real arbitrage calculation
type PriceQuote struct {
	// Buy side (asks) - how much it costs to buy ETH
	BuyPrice    decimal.Decimal // Effective price per ETH when buying
	BuyTotal    decimal.Decimal // Total USDC needed to buy
	BuySlippage decimal.Decimal // Slippage percentage when buying
	BuyFee      decimal.Decimal // Trading fee when buying

	// Sell side (bid) - how much we get when sell ETH
	SellPrice    decimal.Decimal // Effective price per ETH when selling
	SellTotal    decimal.Decimal // Total USDC received when selling
	SellSlippage decimal.Decimal // Slippage percentage when selling
	SellFee      decimal.Decimal // Trading fee when selling

	Amount      decimal.Decimal // Amount of ETH
	Timestamp   time.Time
	BlockNumber uint64 // For DEX quotes
}

// OrderbookLevel represents a single level in the orderbook
type OrderbookLevel struct {
	Price  decimal.Decimal
	Amount decimal.Decimal
}

// Orderbook represents the full orderbook snapshot
type Orderbook struct {
	Bids         []OrderbookLevel
	Asks         []OrderbookLevel
	LastUpdateID int64
	Timestamp    time.Time
}

// Exchange is the interface that both CEX and DEX must implement
type Exchange interface {
	// Name returns the exchange name
	Name() string

	// Type returns the exchange type (CEX or DEX)
	Type() ExchangeType

	// GetPrice returns a price quote for the specified amount of ETH
	GetPrice(ctx context.Context, amountETH decimal.Decimal) (*PriceQuote, error)

	// Start initializes any connections or resources
	Start(ctx context.Context) error

	// Stop cleanly shuts down the exchange
	Stop() error
}

// ArbitrageOpportunity represents a profitable arbitrage
type ArbitrageOpportunity struct {
	Pair                string          // Trading pair (e.g., "ETH-USDC", "WBTC-USDC")
	Direction           string          // "CEX→DEX" or "DEX→CEX"
	Amount              decimal.Decimal // Amount of ETH to trade
	BuyExchange         string
	BuyPrice            decimal.Decimal
	SellExchange        string
	SellPrice           decimal.Decimal
	ProfitUSD           decimal.Decimal
	ProfitPercent       decimal.Decimal
	GasEstimate         decimal.Decimal // Estimated gas cost in USDC
	NetProfit           decimal.Decimal // Profit after gas
	BlockNumber         uint64
	Timestamp           time.Time
	PoolAddress         string          // Uniswap pool address
	RequiredCapitalUSDC decimal.Decimal // Capital needed to execute
	ExpectedOutputUSDC  decimal.Decimal // Expected USDC output
}
