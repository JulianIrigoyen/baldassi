package arbitrage

import (
	"context"
	"fmt"

	"baldassi/internal/exchange"
	"baldassi/internal/gas"
	"baldassi/internal/worker"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
)

// Detector detects arbitrage opportunities between exchanges
type Detector struct {
	cex           exchange.Exchange
	dex           exchange.Exchange
	gasCalculator *gas.GasCalculator
	workerPool    *worker.ArbitrageWorkerPool // Worker pool for parallel processing
}

// NewDetectorWithWorkerPool creates a detector with worker pool for parallel processing
func NewDetectorWithWorkerPool(cex, dex exchange.Exchange, gasCalc *gas.GasCalculator, numWorkers int, pairName, poolAddress string, minProfitUSD decimal.Decimal) *Detector {
	workerPool := worker.NewArbitrageWorkerPool(numWorkers, cex, dex, gasCalc, pairName, poolAddress, minProfitUSD)
	workerPool.Start()

	return &Detector{
		cex:           cex,
		dex:           dex,
		gasCalculator: gasCalc,
		workerPool:    workerPool,
	}
}

// CheckArbitrage checks for arbitrage opportunities for configured trade sizes
func (d *Detector) CheckArbitrage(ctx context.Context, tradeSizes []decimal.Decimal, blockNumber uint64) ([]*exchange.ArbitrageOpportunity, error) {
	// Detector REQUIRES worker pool for parallel processing
	if d.workerPool == nil {
		return nil, fmt.Errorf("detector requires worker pool for arbitrage detection")
	}

	log.Debug().Int("trade_sizes", len(tradeSizes)).Msg("Using worker pool for parallel processing")
	return d.workerPool.CheckArbitrageConcurrent(ctx, tradeSizes, blockNumber)
}

// Stop gracefully shuts down the detector and its worker pool
func (d *Detector) Stop() {
	if d.workerPool != nil {
		log.Debug().Msg("Stopping worker pool")
		d.workerPool.Stop()
	}
}
