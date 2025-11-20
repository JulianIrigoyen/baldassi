package exchange

import (
	"fmt"
	"strings"
)

// FormatArbitrageOpportunity format arbitrage opportunity
func FormatArbitrageOpportunity(opp *ArbitrageOpportunity) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString("=== ARBITRAGE OPPORTUNITY DETECTED ===\n")
	sb.WriteString(fmt.Sprintf("Block Number: %d\n", opp.BlockNumber))
	sb.WriteString(fmt.Sprintf("Timestamp: %s\n", opp.Timestamp.UTC().Format("2006-01-02 15:04:05 UTC")))

	// Format direction
	directionText := ""
	if opp.Direction == "CEX→DEX" {
		directionText = fmt.Sprintf("CEX → DEX (Buy on %s, Sell on %s)", opp.BuyExchange, opp.SellExchange)
	} else {
		directionText = fmt.Sprintf("DEX → CEX (Buy on %s, Sell on %s)", opp.BuyExchange, opp.SellExchange)
	}
	sb.WriteString(fmt.Sprintf("Direction: %s\n", directionText))
	sb.WriteString("\n")

	// Trade details
	sb.WriteString(fmt.Sprintf("Trade Size: %s %s\n",
		opp.Amount.StringFixed(1),
		strings.Split(opp.Pair, "-")[0]))

	sb.WriteString(fmt.Sprintf("CEX Price: $%s (effective with slippage)\n",
		opp.BuyPrice.StringFixed(2)))

	sb.WriteString(fmt.Sprintf("DEX Price: $%s (effective with slippage)\n",
		opp.SellPrice.StringFixed(2)))

	priceDiff := opp.SellPrice.Sub(opp.BuyPrice)

	sb.WriteString(fmt.Sprintf("Price Difference: $%s per %s (%.2f%%)\n",
		priceDiff.StringFixed(2),
		strings.Split(opp.Pair, "-")[0],
		priceDiff.Div(opp.BuyPrice).InexactFloat64()*100))

	sb.WriteString(fmt.Sprintf("Estimated Profit: $%s (before gas)\n", opp.ProfitUSD.StringFixed(2)))
	sb.WriteString(fmt.Sprintf("Gas Cost: $%s\n", opp.GasEstimate.StringFixed(2)))
	sb.WriteString(fmt.Sprintf("Net Profit: $%s (%.2f%%)\n",
		opp.NetProfit.StringFixed(2),
		opp.ProfitPercent.InexactFloat64()))
	sb.WriteString("\n")

	// Execution steps
	sb.WriteString("Execution Steps:\n")

	if opp.Direction == "CEX→DEX" {
		// Buy on CEX, Sell on DEX
		sb.WriteString(fmt.Sprintf("1. Buy %s %s on %s at average price $%s\n",
			opp.Amount.StringFixed(1),
			strings.Split(opp.Pair, "-")[0],
			opp.BuyExchange,
			opp.BuyPrice.StringFixed(2)))
		sb.WriteString("   - Place market order or limit orders consuming top bid/ask levels\n")
		sb.WriteString(fmt.Sprintf("   - Required capital: ~$%s USDC\n", opp.RequiredCapitalUSDC.StringFixed(2)))

		sb.WriteString("2. Transfer ETH to trading wallet\n")

		sb.WriteString(fmt.Sprintf("3. Execute Uniswap V3 swap: %s %s → USDC\n",
			opp.Amount.StringFixed(1),
			strings.Split(opp.Pair, "-")[0]))
		if opp.PoolAddress != "" {
			sb.WriteString(fmt.Sprintf("   - Pool: %s\n", opp.PoolAddress))
		}
		sb.WriteString(fmt.Sprintf("   - Expected output: ~%s USDC (after 0.3%% pool fee)\n",
			opp.ExpectedOutputUSDC.StringFixed(0)))
	} else {
		// Buy on DEX, Sell on CEX
		sb.WriteString(fmt.Sprintf("1. Execute Uniswap V3 swap: USDC → %s %s\n",
			opp.Amount.StringFixed(1),
			strings.Split(opp.Pair, "-")[0]))
		if opp.PoolAddress != "" {
			sb.WriteString(fmt.Sprintf("   - Pool: %s\n", opp.PoolAddress))
		}
		sb.WriteString(fmt.Sprintf("   - Required capital: ~$%s USDC\n", opp.RequiredCapitalUSDC.StringFixed(2)))
		sb.WriteString(fmt.Sprintf("   - Expected output: ~%s %s (after 0.3%% pool fee)\n",
			opp.Amount.StringFixed(1),
			strings.Split(opp.Pair, "-")[0]))

		sb.WriteString("2. Transfer tokens to CEX\n")

		sb.WriteString(fmt.Sprintf("3. Sell %s %s on %s at average price $%s\n",
			opp.Amount.StringFixed(1),
			strings.Split(opp.Pair, "-")[0],
			opp.SellExchange,
			opp.SellPrice.StringFixed(2)))
		sb.WriteString("   - Place market order consuming orderbook levels\n")
		sb.WriteString(fmt.Sprintf("   - Expected revenue: ~$%s USDC\n", opp.ExpectedOutputUSDC.StringFixed(2)))
	}

	sb.WriteString("\n")
	return sb.String()
}
