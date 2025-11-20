package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Ethereum  EthereumConfig
	Binance   BinanceConfig
	Arbitrage ArbitrageConfig
}

type EthereumConfig struct {
	WSURL  string
	RPCURL string

	// Fallback endpoints for automatic failover
	FallbackRPCs []string
	FallbackWSs  []string
}

// GetAllRPCEndpoints returns all RPC endpoints including fallbacks
func (e *EthereumConfig) GetAllRPCEndpoints() []string {
	endpoints := []string{e.RPCURL}
	endpoints = append(endpoints, e.FallbackRPCs...)
	return endpoints
}

// GetAllWSEndpoints returns all WebSocket endpoints including fallbacks
func (e *EthereumConfig) GetAllWSEndpoints() []string {
	endpoints := []string{e.WSURL}
	endpoints = append(endpoints, e.FallbackWSs...)
	return endpoints
}

type BinanceConfig struct {
	APIKey    string
	APISecret string
}

type ArbitrageConfig struct {
	TradeSizes       []float64
	MinProfitUSD     float64
	MinProfitPercent float64
}

// Load explicitly loads .env then reads config
func Load() (*Config, error) {
	// Try to load .env file
	if err := godotenv.Load(); err != nil {
		// Not an error - production won't have .env file
		log.Println("No .env file found, using environment variables")
	} else {
		log.Println("Loaded configuration from .env file")
	}

	cfg := &Config{
		Ethereum: EthereumConfig{
			WSURL:        os.Getenv("ETH_WS_URL"),
			RPCURL:       os.Getenv("ETH_RPC_URL"),
			FallbackRPCs: collectFallbackEndpoints("FALLBACK_RPC_", 5),
			FallbackWSs:  collectFallbackEndpoints("FALLBACK_WS_", 2),
		},
		Binance: BinanceConfig{
			APIKey:    os.Getenv("BINANCE_API_KEY"),
			APISecret: os.Getenv("BINANCE_API_SECRET"),
		},
		Arbitrage: ArbitrageConfig{
			TradeSizes:       parseTradeSizes(os.Getenv("TRADE_SIZES")),
			MinProfitUSD:     parseFloat(os.Getenv("MIN_PROFIT_USD"), 50.0),
			MinProfitPercent: parseFloat(os.Getenv("MIN_PROFIT_PERCENT"), 0.5),
		},
	}

	return cfg, cfg.validate()
}

func (c *Config) validate() error {
	var errors []string

	if c.Ethereum.WSURL == "" {
		errors = append(errors, "ETH_WS_URL is required")
	}
	if c.Ethereum.RPCURL == "" {
		errors = append(errors, "ETH_RPC_URL is required")
	}

	// Note: Binance API key/secret NOT required for public orderbook data

	if len(errors) > 0 {
		return fmt.Errorf("config errors:\n  • %s", strings.Join(errors, "\n  • "))
	}

	return nil
}

// Helper functions
func collectFallbackEndpoints(prefix string, count int) []string {
	var endpoints []string
	for i := 1; i <= count; i++ {
		key := fmt.Sprintf("%s%d", prefix, i)
		if val := os.Getenv(key); val != "" {
			endpoints = append(endpoints, val)
		}
	}
	return endpoints
}

func parseTradeSizes(s string) []float64 {
	if s == "" {
		return []float64{1.0, 10.0, 100.0} // default
	}

	var sizes []float64
	for _, part := range strings.Split(s, ",") {
		if size, err := strconv.ParseFloat(strings.TrimSpace(part), 64); err == nil {
			sizes = append(sizes, size)
		}
	}

	if len(sizes) == 0 {
		return []float64{1.0, 10.0, 100.0}
	}
	return sizes
}

func parseFloat(s string, defaultVal float64) float64 {
	if s == "" {
		return defaultVal
	}
	if val, err := strconv.ParseFloat(s, 64); err == nil {
		return val
	}
	return defaultVal
}

// GetTradingPairs returns the trading pairs configured via TRADING_PAIRS env var
// Example: TRADING_PAIRS=ETH-USDC,WBTC-USDC,LINK-USDC
func GetTradingPairs() []TradingPair {
	pairNames := os.Getenv("TRADING_PAIRS")
	if pairNames == "" {
		// Default to ETH-USDC only
		return []TradingPair{KnownPairs["ETH-USDC"]}
	}

	var pairs []TradingPair
	for _, name := range strings.Split(pairNames, ",") {
		name = strings.TrimSpace(name)
		if pair, exists := KnownPairs[name]; exists {
			pairs = append(pairs, pair)
			// Pair will be logged when created by PairCoordinator
		} else {
			// Unknown pair - log warning
			fmt.Printf("WARNING: Unknown trading pair '%s', skipping. Available: %v\n", name, GetPairNames())
		}
	}

	if len(pairs) == 0 {
		// No valid pairs - use default
		return []TradingPair{KnownPairs["ETH-USDC"]}
	}

	return pairs
}
