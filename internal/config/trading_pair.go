package config

// TradingPair represents a trading pair configuration
type TradingPair struct {
	Name          string      // "ETH-USDC"
	BaseToken     Token       // The base token (e.g., ETH)
	QuoteToken    Token       // The quote token (e.g., USDC)
	UniswapPool   PoolConfig  // Uniswap V3 pool configuration
	BinanceSymbol string      // Binance API symbol (e.g., "ETHUSDC")
}

// Token represents an ERC-20 token on Ethereum
type Token struct {
	Address  string // Contract address
	Decimals int    // Token decimals
	Symbol   string // Token symbol
}

// PoolConfig represents Uniswap V3 pool configuration
type PoolConfig struct {
	Address string // Pool contract address
	Fee     int    // Fee tier (500 = 0.05%, 3000 = 0.3%, 10000 = 1%)
}

// KnownPairs is a registry of pre-configured trading pairs
// Users can enable these pairs via TRADING_PAIRS env var
var KnownPairs = map[string]TradingPair{
	"ETH-USDC": {
		Name: "ETH-USDC",
		BaseToken: Token{
			Address:  "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2", // WETH
			Decimals: 18,
			Symbol:   "WETH",
		},
		QuoteToken: Token{
			Address:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
			Decimals: 6,
			Symbol:   "USDC",
		},
		UniswapPool: PoolConfig{
			Address: "0x88e6A0c2dDD26FEEb64F039a2c41296FcB3f5640", // ETH-USDC 0.05%
			Fee:     500,                                         // 0.05%
		},
		BinanceSymbol: "ETHUSDC",
	},

	"USDT-USDC": {
		Name: "USDT-USDC",
		BaseToken: Token{
			Address:  "0xdAC17F958D2ee523a2206206994597C13D831ec7", // USDT
			Decimals: 6,
			Symbol:   "USDT",
		},
		QuoteToken: Token{
			Address:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
			Decimals: 6,
			Symbol:   "USDC",
		},
		UniswapPool: PoolConfig{
			Address: "0x3416cF6C708Da44DB2624D63ea0AAef7113527C6", // USDT-USDC 0.01%
			Fee:     100,                                         // 0.01% (low fee for stable pairs)
		},
		BinanceSymbol: "USDTUSDC",
	},

	"MATIC-USDC": {
		Name: "MATIC-USDC",
		BaseToken: Token{
			Address:  "0x7D1AfA7B718fb893dB30A3aBc0Cfc608AaCfeBB0", // MATIC
			Decimals: 18,
			Symbol:   "MATIC",
		},
		QuoteToken: Token{
			Address:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
			Decimals: 6,
			Symbol:   "USDC",
		},
		UniswapPool: PoolConfig{
			Address: "0xA374094527e1673A86dE625aa59517c5dE346d32", // MATIC-USDC 0.05%
			Fee:     500,                                         // 0.05%
		},
		BinanceSymbol: "MATICUSDC",
	},

	"LINK-USDC": {
		Name: "LINK-USDC",
		BaseToken: Token{
			Address:  "0x514910771AF9Ca656af840dff83E8264EcF986CA", // LINK
			Decimals: 18,
			Symbol:   "LINK",
		},
		QuoteToken: Token{
			Address:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
			Decimals: 6,
			Symbol:   "USDC",
		},
		UniswapPool: PoolConfig{
			Address: "0xfAD57d2039C21811C8F2B5D5B65308aa99D31559", // LINK-USDC 0.3%
			Fee:     3000,                                        // 0.3%
		},
		BinanceSymbol: "LINKUSDC",
	},

	"UNI-USDC": {
		Name: "UNI-USDC",
		BaseToken: Token{
			Address:  "0x1f9840a85d5aF5bf1D1762F925BDADdC4201F984", // UNI
			Decimals: 18,
			Symbol:   "UNI",
		},
		QuoteToken: Token{
			Address:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
			Decimals: 6,
			Symbol:   "USDC",
		},
		UniswapPool: PoolConfig{
			Address: "0xD0fC8bA7E267f2bc56044A7715A489d851dC6D78", // UNI-USDC 0.3%
			Fee:     3000,                                        // 0.3%
		},
		BinanceSymbol: "UNIUSDC",
	},

	"AAVE-USDC": {
		Name: "AAVE-USDC",
		BaseToken: Token{
			Address:  "0x7Fc66500c84A76Ad7e9c93437bFc5Ac33E2DDaE9", // AAVE
			Decimals: 18,
			Symbol:   "AAVE",
		},
		QuoteToken: Token{
			Address:  "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", // USDC
			Decimals: 6,
			Symbol:   "USDC",
		},
		UniswapPool: PoolConfig{
			Address: "0x5aB53EE1d50eeF2C1DD3d5402789cd27bB52c1bB", // AAVE-USDC 0.3%
			Fee:     3000,                                        // 0.3%
		},
		BinanceSymbol: "AAVEUSDC",
	},
}

// GetPairNames returns all available pair names
func GetPairNames() []string {
	names := make([]string, 0, len(KnownPairs))
	for name := range KnownPairs {
		names = append(names, name)
	}
	return names
}
