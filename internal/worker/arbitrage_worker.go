package worker

import (
	"context"
	"fmt"
	"sync"

	"baldassi/internal/exchange"
	"baldassi/internal/gas"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

// ArbitrageOpportunityChecker represents a single arbitrage check job for a specific trade size
type ArbitrageOpportunityChecker struct {
	CEX           exchange.Exchange
	DEX           exchange.Exchange
	GasCalculator *gas.GasCalculator
	TradeSize     decimal.Decimal
	BlockNumber   uint64
	PairName      string          // Trading pair name (e.g., "ETH-USDC")
	PoolAddress   string          // Uniswap pool address
	MinProfitUSD  decimal.Decimal // Minimum profit threshold from config

	// Result handling
	resultMutex   *sync.Mutex
	opportunities *[]*exchange.ArbitrageOpportunity
}

// NewArbitrageOpportunityChecker creates a new arbitrage checking job
func NewArbitrageOpportunityChecker(
	cex exchange.Exchange,
	dex exchange.Exchange,
	gasCalc *gas.GasCalculator,
	size decimal.Decimal,
	block uint64,
	pairName string,
	poolAddress string,
	minProfitUSD decimal.Decimal,
	resultMutex *sync.Mutex,
	results *[]*exchange.ArbitrageOpportunity,
) *ArbitrageOpportunityChecker {
	return &ArbitrageOpportunityChecker{
		CEX:           cex,
		DEX:           dex,
		GasCalculator: gasCalc,
		TradeSize:     size,
		BlockNumber:   block,
		PairName:      pairName,
		PoolAddress:   poolAddress,
		MinProfitUSD:  minProfitUSD,
		resultMutex:   resultMutex,
		opportunities: results,
	}
}

// Execute implements the Job interface - checks for arbitrage on this trade size
func (j *ArbitrageOpportunityChecker) Execute(ctx context.Context) error {
	log.Debug().
		Str("pair", j.PairName).
		Str("amount", j.TradeSize.StringFixed(2)).
		Uint64("block", j.BlockNumber).
		Msg("Worker starting arbitrage check")

	// Get prices from both exchanges concurrently
	type result struct {
		exchange string
		quote    *exchange.PriceQuote
		err      error
	}

	resultChan := make(chan result, 2)

	// Fetch CEX price
	go func() {
		quote, err := j.CEX.GetPrice(ctx, j.TradeSize)
		resultChan <- result{"cex", quote, err}
	}()

	// Fetch DEX price
	go func() {
		quote, err := j.DEX.GetPrice(ctx, j.TradeSize)
		resultChan <- result{"dex", quote, err}
	}()

	// Collect results
	var cexQuote, dexQuote *exchange.PriceQuote
	for i := 0; i < 2; i++ {
		res := <-resultChan
		if res.err != nil {
			log.Error().
				Err(res.err).
				Str("pair", j.PairName).
				Str("exchange", res.exchange).
				Str("amount", j.TradeSize.StringFixed(2)).
				Uint64("block", j.BlockNumber).
				Msg("Failed to get price from exchange")
			return fmt.Errorf("failed to get price from %s for size %s: %w",
				res.exchange, j.TradeSize.String(), res.err)
		}
		if res.exchange == "cex" {
			cexQuote = res.quote
		} else {
			dexQuote = res.quote
		}
	}

	log.Debug().
		Str("pair", j.PairName).
		Str("amount", j.TradeSize.StringFixed(2)).
		Str("cex_buy", cexQuote.BuyPrice.StringFixed(2)).
		Str("cex_sell", cexQuote.SellPrice.StringFixed(2)).
		Str("dex_buy", dexQuote.BuyPrice.StringFixed(2)).
		Str("dex_sell", dexQuote.SellPrice.StringFixed(2)).
		Uint64("block", j.BlockNumber).
		Msg("Fetched prices from both exchanges")

	// Check arbitrage using actual bid/ask prices
	var localOpps []*exchange.ArbitrageOpportunity

	// CEX→DEX: Buy at CEX ASK, Sell at DEX BID
	// Calculate profit components explicitly
	profit := j.calculateProfit(ctx, cexQuote, dexQuote, j.TradeSize, "CEX→DEX")
	if profit != nil {
		// Explicit profit pipeline
		spreadPerUnit := dexQuote.SellPrice.Sub(cexQuote.BuyPrice)
		grossUSD := profit.ProfitUSD
		gasUSD := profit.GasEstimate
		netUSD := profit.NetProfit
		profitPercent := profit.ProfitPercent

		log.Debug().
			Str("pair", j.PairName).
			Str("amount", j.TradeSize.StringFixed(2)).
			Str("direction", "CEX→DEX").
			Str("spread_per_unit", spreadPerUnit.StringFixed(4)).
			Str("gross_usd", grossUSD.StringFixed(2)).
			Str("gas_usd", gasUSD.StringFixed(2)).
			Str("net_usd", netUSD.StringFixed(2)).
			Str("profit_percent", profitPercent.StringFixed(4)).
			Bool("profitable", netUSD.GreaterThan(j.MinProfitUSD)).
			Msg("CEX→DEX evaluation")

		if netUSD.GreaterThan(j.MinProfitUSD) {
			log.Info().
				Str("pair", j.PairName).
				Str("amount", j.TradeSize.StringFixed(2)).
				Str("net_profit", netUSD.StringFixed(2)).
				Msg("CEX→DEX opportunity found!")
			profit.BlockNumber = j.BlockNumber
			localOpps = append(localOpps, profit)
		}
	}

	// DEX→CEX: Buy DEX ask, Sell CEX bid
	profit = j.calculateProfit(ctx, dexQuote, cexQuote, j.TradeSize, "DEX→CEX")
	if profit != nil {
		// Explicit profit pipeline
		spreadPerUnit := cexQuote.SellPrice.Sub(dexQuote.BuyPrice)
		grossUSD := profit.ProfitUSD
		gasUSD := profit.GasEstimate
		netUSD := profit.NetProfit
		profitPercent := profit.ProfitPercent

		log.Debug().
			Str("pair", j.PairName).
			Str("amount", j.TradeSize.StringFixed(2)).
			Str("direction", "DEX→CEX").
			Str("spread_per_unit", spreadPerUnit.StringFixed(4)).
			Str("gross_usd", grossUSD.StringFixed(2)).
			Str("gas_usd", gasUSD.StringFixed(2)).
			Str("net_usd", netUSD.StringFixed(2)).
			Str("profit_percent", profitPercent.StringFixed(4)).
			Bool("profitable", netUSD.GreaterThan(j.MinProfitUSD)).
			Msg("DEX→CEX evaluation")

		if netUSD.GreaterThan(j.MinProfitUSD) {
			log.Info().
				Str("pair", j.PairName).
				Str("amount", j.TradeSize.StringFixed(2)).
				Str("net_profit", netUSD.StringFixed(2)).
				Msg("DEX→CEX opportunity found!")
			profit.BlockNumber = j.BlockNumber
			localOpps = append(localOpps, profit)
		}
	}

	// Add opportunities to shared results (thread-safe)
	if len(localOpps) > 0 {
		j.resultMutex.Lock()
		*j.opportunities = append(*j.opportunities, localOpps...)
		j.resultMutex.Unlock()
	}

	return nil
}

// calculateProfit calculates profit using bid/ask prices
func (j *ArbitrageOpportunityChecker) calculateProfit(ctx context.Context, buyQuote, sellQuote *exchange.PriceQuote, amount decimal.Decimal, direction string) *exchange.ArbitrageOpportunity {
	var buyTotal, sellRevenue decimal.Decimal
	var buyExchange, sellExchange string
	var buyPrice, sellPrice decimal.Decimal

	if direction == "CEX→DEX" {
		// Buy on CEX at ASK price
		buyTotal = buyQuote.BuyTotal
		buyPrice = buyQuote.BuyPrice
		buyExchange = j.CEX.Name()

		// Sell on DEX at BID price
		sellRevenue = sellQuote.SellTotal
		sellPrice = sellQuote.SellPrice
		sellExchange = j.DEX.Name()
	} else { // DEX→CEX
		// Buy on DEX at ASK price
		buyTotal = buyQuote.BuyTotal
		buyPrice = buyQuote.BuyPrice
		buyExchange = j.DEX.Name()

		// Sell on CEX at BID price
		sellRevenue = sellQuote.SellTotal
		sellPrice = sellQuote.SellPrice
		sellExchange = j.CEX.Name()
	}

	// Calculate profit (sell revenue - buy cost)
	// Fees are already included in BuyTotal and SellTotal
	profitBeforeGas := sellRevenue.Sub(buyTotal)

	// Sanity check: compare gross with simple spread calculation
	simpleSpread := sellPrice.Sub(buyPrice)
	expectedGrossFromSpread := simpleSpread.Mul(amount)

	log.Debug().
		Str("pair", j.PairName).
		Str("direction", direction).
		Str("amount", amount.StringFixed(2)).
		Str("simple_spread", simpleSpread.StringFixed(4)).
		Str("expected_gross_from_spread", expectedGrossFromSpread.StringFixed(2)).
		Str("actual_gross_usd", profitBeforeGas.StringFixed(2)).
		Str("buy_total_with_fees", buyTotal.StringFixed(2)).
		Str("sell_revenue_after_fees", sellRevenue.StringFixed(2)).
		Str("fees_impact", profitBeforeGas.Sub(expectedGrossFromSpread).StringFixed(2)).
		Msg("Profit calculation breakdown")

	// Get calculated gas cost
	gasEstimate := decimal.NewFromFloat(30) // use defa
	if j.GasCalculator != nil {
		calculatedGas, err := j.GasCalculator.CalculateSwapCostUSD(ctx)
		if err == nil {
			gasEstimate = calculatedGas
		} else {
			fmt.Printf("Warning: Failed to calculate gas for size %s: %v\n",
				j.TradeSize.StringFixed(2), err)
		}
	}

	// Subtract gas costs
	netProfit := profitBeforeGas.Sub(gasEstimate)

	// Calculate profit
	profitPercent := decimal.Zero
	if buyTotal.GreaterThan(decimal.Zero) {
		profitPercent = netProfit.Div(buyTotal).Mul(decimal.NewFromInt(100))
	}

	// Return the arbitrage opportunity with bid/ask prices
	return &exchange.ArbitrageOpportunity{
		Pair:                j.PairName,
		Direction:           direction,
		Amount:              amount,
		BuyExchange:         buyExchange,
		BuyPrice:            buyPrice,
		SellExchange:        sellExchange,
		SellPrice:           sellPrice,
		ProfitUSD:           profitBeforeGas,
		ProfitPercent:       profitPercent,
		GasEstimate:         gasEstimate,
		NetProfit:           netProfit,
		Timestamp:           buyQuote.Timestamp,
		BlockNumber:         j.BlockNumber,
		PoolAddress:         j.PoolAddress,
		RequiredCapitalUSDC: buyTotal,
		ExpectedOutputUSDC:  sellRevenue,
	}
}

// ArbitrageWorkerPool manages a pool of workers for parallel arbitrage checking
type ArbitrageWorkerPool struct {
	pool          *Pool
	cex           exchange.Exchange
	dex           exchange.Exchange
	gasCalculator *gas.GasCalculator
	pairName      string
	poolAddress   string
	minProfitUSD  decimal.Decimal
}

// NewArbitrageWorkerPool creates a new arbitrage-specific worker pool
func NewArbitrageWorkerPool(numWorkers int, cex, dex exchange.Exchange, gasCalc *gas.GasCalculator, pairName, poolAddress string, minProfitUSD decimal.Decimal) *ArbitrageWorkerPool {
	return &ArbitrageWorkerPool{
		pool:          NewPool(numWorkers),
		cex:           cex,
		dex:           dex,
		gasCalculator: gasCalc,
		pairName:      pairName,
		poolAddress:   poolAddress,
		minProfitUSD:  minProfitUSD,
	}
}

// Start init worker pool
func (a *ArbitrageWorkerPool) Start() {
	a.pool.Start()
}

// CheckArbitrageConcurrent checks multiple trade sizes in parallel
func (a *ArbitrageWorkerPool) CheckArbitrageConcurrent(ctx context.Context, tradeSizes []decimal.Decimal, blockNumber uint64) ([]*exchange.ArbitrageOpportunity, error) {
	// Shared result storage with mutex for thread safety
	var opportunities []*exchange.ArbitrageOpportunity
	var resultMutex sync.Mutex
	var firstError error
	var errorMutex sync.Mutex

	// Create jobs for each trade size
	jobs := make([]Job, len(tradeSizes))
	for i, size := range tradeSizes {
		jobs[i] = NewArbitrageOpportunityChecker(
			a.cex,
			a.dex,
			a.gasCalculator,
			size,
			blockNumber,
			a.pairName,
			a.poolAddress,
			a.minProfitUSD,
			&resultMutex,
			&opportunities,
		)
	}

	// Execute all checks
	var wg sync.WaitGroup
	wg.Add(len(jobs))

	for _, job := range jobs {
		go func(j Job) {
			defer wg.Done()
			if err := j.Execute(ctx); err != nil {
				log.Error().
					Err(err).
					Str("job_type", "arbitrage_check").
					Msg("Job execution failed")

				errorMutex.Lock()
				if firstError == nil {
					firstError = err
				}
				errorMutex.Unlock()
			}
		}(job)
	}

	wg.Wait()

	// Return first error if any job failed
	if firstError != nil {
		log.Error().
			Err(firstError).
			Int("successful_opportunities", len(opportunities)).
			Msg("Worker pool encountered errors during execution")
		return opportunities, firstError
	}

	return opportunities, nil
}

// Stop gracefully shuts down the worker pool
func (a *ArbitrageWorkerPool) Stop() {
	a.pool.Stop()
}
