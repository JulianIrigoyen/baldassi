# CEX-DEX Arbitrage Bot

Multi-pair arbitrage detection between Binance and Uniswap V3. Monitors ETH-USDC, LINK-USDC, and AAVE-USDC. Block-driven requoting for atomic consistency.

See [Challenge.md](challenge/Challenge.md) for the original requirements.

## What It Does

Watches Ethereum blocks via WebSocket. On each block:
1. Fetches Binance orderbooks (REST API)
2. Fetches Uniswap quotes (QuoterV2 contract via RPC)
3. Compares prices across 3 trade sizes (1, 10, 100 tokens)
4. Calculates profit after gas + fees
5. Prints opportunities if profitable

Currently finds zero real arb.

## Validation

```bash
# Run multi-pair bot (production)
go run cmd/bot_multipair/main.go

# Run with debug logs
LOG_LEVEL=debug go run cmd/bot_multipair/main.go

# Run integration tests
go run cmd/test_multipair/main.go        # Test multipair bot
go run cmd/test_binance_orderbook/main.go  # Test CEX orderbook depth
go run cmd/test_uniswap_quoter/main.go   # Test DEX price quotes
go run  cmd/test_resiliency/main.go    # Test connection resiliency
```

## Architecture

### [Rendered Sequence Diagram](https://www.mermaidchart.com/app/projects/8e4674bb-43d7-4781-bcbe-98c351da658b/diagrams/cc5509f2-7bea-4b70-8865-c22e16b6b453/share/invite/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJkb2N1bWVudElEIjoiY2M1NTA5ZjItN2JlYS00YjcwLTg4NjUtYzIyZTE2YjZiNDUzIiwiYWNjZXNzIjoiRWRpdCIsImlhdCI6MTc2MzYwOTk5MX0.dyjf8mK6ZQpKXxGjcXyjdn_z1ZIh8k6yeOHdjlGIJyk)

```mermaid
sequenceDiagram
      participant Main as main()
      participant Config as Config
      participant RPC as RpcManager (Shared)
      participant Binance as Binance Clients
      participant Uniswap as Uniswap Clients
      participant Gas as Gas Calculator
      participant Detector as Arbitrage Detectors
      participant Monitor as Block Monitor
      participant Worker as Worker Pool

      Note over Main: STARTUP PHASE
      Main->>Config: Load configuration
      Config-->>Main: Trading pairs, RPC URLs

      Main->>RPC: NewRpcManager(endpoints)
      Note over RPC: Connects to 6 RPC endpoints<br/>SHARED by all components
      RPC-->>Main: Single RPC manager instance

      loop For each trading pair
          Main->>Binance: NewBinanceClient(symbol)
          Binance-->>Main: Binance client

          Main->>Uniswap: NewUniswapClientForPair(sharedRPC, pair)
          Note over Uniswap: Uses shared RPC<br/>No new connection
          Uniswap-->>Main: Uniswap client
      end

      Main->>Gas: NewGasCalculator(sharedRPC, ethPriceFunc)
      Note over Gas: Uses shared RPC<br/>No new connection
      Gas-->>Main: Gas calculator

      loop For each trading pair
          Main->>Detector: NewDetectorWithWorkerPool(binance, uniswap, gas)
          Detector->>Worker: Create worker pool (3 workers)
          Worker-->>Detector: Worker pool ready
          Detector-->>Main: Detector ready
      end

      Main->>Monitor: Start WebSocket block monitor
      Monitor-->>Main: Subscribed to newHeads

      Note over Main: RUNTIME PHASE - Block Event Loop

      loop Each New Block
          Monitor->>Main: New block event

          Main->>Gas: UpdateOnBlock(blockNum)
          Note over Gas: Async RPC call via shared manager

          Main->>Uniswap: UpdateBlock(blockNum)
          Note over Uniswap: Invalidate block cache

          par For each trading pair (parallel)
              Main->>Detector: CheckArbitrage(tradeSizes, blockNum)
              Detector->>Worker: CheckArbitrageConcurrent(tradeSizes)

              par For each trade size (parallel)
                  Worker->>Worker: Create job for size

                  par Fetch prices in parallel
                      Worker->>Binance: GetPrice(amount)
                      Note over Binance: HTTP API call<br/>Token bucket rate limit
                      Binance-->>Worker: CEX quote (buy/sell)

                      Worker->>Uniswap: GetPrice(amount)
                      Uniswap->>RPC: CallContract(QuoterV2)
                      Note over RPC: eth_call via shared RPC<br/>Block-scoped cache
                      RPC-->>Uniswap: Quote result
                      Uniswap-->>Worker: DEX quote (buy/sell)
                  end

                  Worker->>Worker: Calculate CEX→DEX spread
                  Worker->>Worker: Calculate DEX→CEX spread
                  Worker->>Gas: CalculateSwapCostUSD()
                  Gas->>RPC: Get gas price
                  RPC-->>Gas: Base fee + priority
                  Gas-->>Worker: Gas cost in USD

                  Worker->>Worker: Net profit = spread - gas
                  alt Net profit > threshold
                      Worker->>Worker: Append to opportunities
                  end
              end

              Worker-->>Detector: Opportunities array
              Detector-->>Main: Opportunities for pair
          end

          Main->>Main: Filter and format opportunities
          Main->>Main: Print to stdout + log
          Main->>Main: Save checkpoint (every 5 blocks)
      end
```

### Key Decisions

**Challenge: CEX Orderbook Integration** → `internal/exchange/binance.go`
- REST API orderbook depth calc
- Walks bid/ask levels for effective execution price
- Rate limited at 20 req/sec via token bucket (`internal/ratelimit/token_bucket.go`)
- 5s cache TTL (arb needs fresh data)

**Challenge: Block Streaming** → `internal/monitor/block.go`
- WebSocket `eth_subscribe("newHeads")`
- Exponential backoff reconnect (1s-60s + jitter)
- Checkpoint every 5 blocks (`./data/checkpoint.json`)
- Fallback to next WS endpoint on failure

**Challenge: DEX Price** → `internal/exchange/uniswap.go`
- QuoterV2 `quoteExactInputSingle` / `quoteExactOutputSingle`
- Block-scoped cache (invalidated on new block)
- Shared RPC manager across all Uniswap clients (`internal/ethereum/rpc_manager.go`)
  - **Why shared?** 6 RPC endpoints, 3 pairs → 18 connections if separate → 6 connections if shared
  - Automatic failover, connection pooling, mutex-protected

**Challenge: Arbitrage Logic** → `internal/arbitrage/detector.go` + `internal/worker/arbitrage_worker.go`
- Worker pool (3 workers per pair)
- Parallel price fetching (CEX + DEX in goroutines)
- Gas estimation (`internal/gas/calculator.go`): 150k gas units × (baseFee + priorityFee) × ETH price
- Net profit = spread - gas - fees
- Prints if > $1 OR > 0.01%

**Challenge: Caching** → `internal/cache/`
- Block-scoped: Uniswap quotes, ETH price (invalidated on new block)
- TTL-based: Binance orderbooks (5s), gas data (12s)
- Thread-safe with `sync.RWMutex`

**Challenge: Concurrency** → Worker pools + channels
- 3 pairs × 3 sizes = 9 workers per block
- Goroutines for parallel CEX/DEX fetching
- Context cancellation for shutdown

### Why Separate Binance Clients?

Each pair has its own `BinanceClient` because:
- Symbol-specific: `ETHUSDC` ≠ `LINKUSDC` ≠ `AAVEUSDC`
- Cannot share cache (different orderbooks)
- HTTP connections auto-reused by Go's `http.Client`
- Separate rate limiters = better parallelism (20 req/sec each)

Ethereum RPC is shared because:
- Same data for all clients (blocks, gas prices)
- Persistent TCP connections (expensive to duplicate)
- 4× connection reduction (24 → 6)

## Setup

```bash
# Copy env template
cp .env.example .env

# Add your RPC URLs
nano .env

# Run
go run cmd/bot_multipair/main.go

# Debug mode (verbose)
LOG_LEVEL=debug go run cmd/bot_multipair/main.go
```

## Config

`.env`:
```bash
ETH_WS_URL=wss://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
ETH_RPC_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_KEY
FALLBACK_RPC_1=https://your-quicknode.quiknode.pro/KEY/
# ... up to FALLBACK_RPC_5

TRADE_SIZES=1.0,10.0,100.0
MIN_PROFIT_USD=1.0
MIN_PROFIT_PERCENT=0.01
```

## Tests

```bash
# All tests
go test ./... -v

# With race detector
go test ./... -race

# Specific package
go test ./internal/gas -v
```

Test coverage:
- `internal/ethereum/*_test.go` - RPC failover, mutex safety
- `internal/exchange/*_test.go` - Binance/Uniswap price fetching
- `internal/gas/calculator_test.go` - Gas calculation math
- `internal/worker/*_test.go` - Worker pool concurrency
- `internal/ratelimit/token_bucket_test.go` - Rate limiting

## Output

When profitable (extremelly unlikley and not expected):
```
WRN ARBITRAGE OPPORTUNITY FOUND
  pair=ETH-USDC
  direction=CEX→DEX
  amount=10
  buy_exchange=Binance buy_price=3096.45
  sell_exchange=Uniswap sell_price=3116.78
  profit_usd=51.27 profit_percent=0.68
  gas_cost=15.23
  block=23825986
```

## Project Structure

```
cmd/
  bot/               - Single-pair bot
  bot_multipair/     - Main multi-pair bot
  test_*/            - Integration tests
internal/
  arbitrage/         - Opportunity detection logic
  cache/             - Block-scoped + TTL caching
  config/            - Env var loading + validation
  ethereum/          - RPC manager with failover
  exchange/          - Binance + Uniswap clients
  gas/               - Gas cost calculator
  logger/            - Structured logging (zerolog)
  monitor/           - WebSocket block monitoring
  ratelimit/         - Token bucket rate limiter
  worker/            - Worker pool for concurrency
```

## What Works

✅ WebSocket block streaming with reconnect
✅ Multi-RPC failover (6 endpoints)
✅ Concurrent pair processing (3 pairs in parallel)
✅ Parallel trade size checks (3 workers per pair)
✅ Shared RPC manager (6 connections instead of 18)
✅ Block-scoped cache invalidation
✅ Checkpoint recovery
✅ Rate limiting
✅ Tests pass (no race conditions)

## Known Limitations

No real arbitrage opportunities found after gas costs. The market is efficient.
