package analyzer

import (
	"encoding/json"
	"fmt"
	"strings"

	"bitso-trading-platform/backtesting/internal/models"
)

// GenerateTextReport generates a human-readable text report (exported for API use).
func GenerateTextReport(result *models.BacktestResult) string {
	var sb strings.Builder

	sb.WriteString("=" + strings.Repeat("=", 70) + "\n")
	sb.WriteString("  BACKTEST REPORT\n")
	sb.WriteString("=" + strings.Repeat("=", 70) + "\n\n")

	sb.WriteString(fmt.Sprintf("Backtest ID: %s\n", result.BacktestID))
	sb.WriteString(fmt.Sprintf("Status: %s\n", result.Status))
	sb.WriteString(fmt.Sprintf("Duration: %d seconds\n", result.Duration))
	if result.Config != nil {
		sb.WriteString("\nSTRATEGY & PARAMETERS\n")
		sb.WriteString(strings.Repeat("-", 70) + "\n")
		c := result.Config
		sb.WriteString(fmt.Sprintf("Name:                  %s\n", c.Name))
		sb.WriteString(fmt.Sprintf("Book:                  %s\n", c.Book))
		sb.WriteString(fmt.Sprintf("Strategy:              %s\n", c.Strategy))
		sb.WriteString(fmt.Sprintf("Start Date:            %s\n", c.StartDate.Format("2006-01-02")))
		sb.WriteString(fmt.Sprintf("End Date:              %s\n", c.EndDate.Format("2006-01-02")))
		sb.WriteString(fmt.Sprintf("Initial Balance:       $%.2f\n", c.InitialBalance))
		sb.WriteString(fmt.Sprintf("Slippage:              %s %.4f\n", c.SlippageModel, c.SlippageValue))
		sb.WriteString(fmt.Sprintf("Commission Rate:       %.4f\n", c.CommissionRate))
		if len(c.StrategyParams) > 0 {
			paramsJSON, _ := json.Marshal(c.StrategyParams)
			sb.WriteString(fmt.Sprintf("Strategy Params:       %s\n", string(paramsJSON)))
		}
		sb.WriteString("\n")
	}
	if result.MetThresholds || result.FailureReason != "" {
		sb.WriteString("SUCCESS CRITERIA\n")
		sb.WriteString(strings.Repeat("-", 70) + "\n")
		sb.WriteString(fmt.Sprintf("Met Thresholds:        %v\n", result.MetThresholds))
		if result.FailureReason != "" {
			sb.WriteString(fmt.Sprintf("Failure Reason:         %s\n", result.FailureReason))
		}
		sb.WriteString("\n")
	}

	if result.Summary == nil {
		sb.WriteString("No performance summary available.\n")
		return sb.String()
	}

	s := result.Summary

	// Performance Summary
	sb.WriteString("PERFORMANCE SUMMARY\n")
	sb.WriteString(strings.Repeat("-", 70) + "\n")
	sb.WriteString(fmt.Sprintf("Initial Balance:        $%.2f\n", s.InitialBalance))
	sb.WriteString(fmt.Sprintf("Final Balance:          $%.2f\n", s.FinalBalance))
	sb.WriteString(fmt.Sprintf("Total Return:           $%.2f (%.2f%%)\n", s.TotalReturn, s.TotalReturnPercent))
	sb.WriteString(fmt.Sprintf("Annualized Return:      %.2f%%\n", s.AnnualizedReturn*100))
	sb.WriteString(fmt.Sprintf("Peak Balance:           $%.2f\n\n", s.PeakBalance))

	// Risk Metrics
	sb.WriteString("RISK METRICS\n")
	sb.WriteString(strings.Repeat("-", 70) + "\n")
	sb.WriteString(fmt.Sprintf("Volatility:             %.4f\n", s.Volatility))
	sb.WriteString(fmt.Sprintf("Sharpe Ratio:           %.2f\n", s.SharpeRatio))
	sb.WriteString(fmt.Sprintf("Sortino Ratio:          %.2f\n", s.SortinoRatio))
	sb.WriteString(fmt.Sprintf("Max Drawdown:           $%.2f (%.2f%%)\n\n", s.MaxDrawdown, s.MaxDrawdownPercent))

	// Trade Statistics
	sb.WriteString("TRADE STATISTICS\n")
	sb.WriteString(strings.Repeat("-", 70) + "\n")
	sb.WriteString(fmt.Sprintf("Total Trades:           %d\n", s.TotalTrades))
	sb.WriteString(fmt.Sprintf("Winning Trades:         %d\n", s.WinningTrades))
	sb.WriteString(fmt.Sprintf("Losing Trades:          %d\n", s.LosingTrades))
	sb.WriteString(fmt.Sprintf("Win Rate:               %.2f%%\n", s.WinRate*100))
	sb.WriteString(fmt.Sprintf("Profit Factor:          %.2f\n", s.ProfitFactor))
	sb.WriteString(fmt.Sprintf("Average Win:            $%.2f\n", s.AverageWin))
	sb.WriteString(fmt.Sprintf("Average Loss:           $%.2f\n", s.AverageLoss))
	sb.WriteString(fmt.Sprintf("Avg Holding Time:       %d seconds\n\n", s.AverageHoldingTime))

	// P&L Breakdown
	sb.WriteString("P&L BREAKDOWN\n")
	sb.WriteString(strings.Repeat("-", 70) + "\n")
	sb.WriteString(fmt.Sprintf("Gross P&L:              $%.2f\n", s.GrossProfitLoss))
	sb.WriteString(fmt.Sprintf("Total Commissions:      $%.2f\n", s.TotalCommissions))
	sb.WriteString(fmt.Sprintf("Net P&L:                $%.2f\n\n", s.NetProfitLoss))

	sb.WriteString("=" + strings.Repeat("=", 70) + "\n")

	return sb.String()
}

// generateJSONReport generates a JSON report
func generateJSONReport(result *models.BacktestResult) ([]byte, error) {
	return json.MarshalIndent(result, "", "  ")
}

// GenerateHTMLReport generates an HTML report (exported for API use).
func GenerateHTMLReport(result *models.BacktestResult) string {
	var sb strings.Builder

	sb.WriteString("<!DOCTYPE html>\n")
	sb.WriteString("<html><head><title>Backtest Report</title>\n")
	sb.WriteString("<style>\n")
	sb.WriteString("body { font-family: Arial, sans-serif; margin: 20px; }\n")
	sb.WriteString("table { border-collapse: collapse; width: 100%; margin: 20px 0; }\n")
	sb.WriteString("th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }\n")
	sb.WriteString("th { background-color: #4CAF50; color: white; }\n")
	sb.WriteString(".positive { color: green; }\n")
	sb.WriteString(".negative { color: red; }\n")
	sb.WriteString("</style>\n")
	sb.WriteString("</head><body>\n")

	sb.WriteString("<h1>Backtest Report</h1>\n")
	sb.WriteString(fmt.Sprintf("<p><strong>Backtest ID:</strong> %s</p>\n", result.BacktestID))
	sb.WriteString(fmt.Sprintf("<p><strong>Status:</strong> %s</p>\n", result.Status))

	if result.Config != nil {
		c := result.Config
		sb.WriteString("<h2>Strategy & Parameters</h2>\n")
		sb.WriteString("<table>\n")
		sb.WriteString("<tr><th>Parameter</th><th>Value</th></tr>\n")
		sb.WriteString(fmt.Sprintf("<tr><td>Name</td><td>%s</td></tr>\n", c.Name))
		sb.WriteString(fmt.Sprintf("<tr><td>Book</td><td>%s</td></tr>\n", c.Book))
		sb.WriteString(fmt.Sprintf("<tr><td>Strategy</td><td>%s</td></tr>\n", c.Strategy))
		sb.WriteString(fmt.Sprintf("<tr><td>Start Date</td><td>%s</td></tr>\n", c.StartDate.Format("2006-01-02")))
		sb.WriteString(fmt.Sprintf("<tr><td>End Date</td><td>%s</td></tr>\n", c.EndDate.Format("2006-01-02")))
		sb.WriteString(fmt.Sprintf("<tr><td>Initial Balance</td><td>$%.2f</td></tr>\n", c.InitialBalance))
		if len(c.StrategyParams) > 0 {
			paramsJSON, _ := json.Marshal(c.StrategyParams)
			sb.WriteString(fmt.Sprintf("<tr><td>Strategy Params</td><td><code>%s</code></td></tr>\n", string(paramsJSON)))
		}
		sb.WriteString("</table>\n")
	}
	if result.MetThresholds || result.FailureReason != "" {
		sb.WriteString("<h2>Success Criteria</h2>\n")
		sb.WriteString(fmt.Sprintf("<p><strong>Met Thresholds:</strong> %v</p>\n", result.MetThresholds))
		if result.FailureReason != "" {
			sb.WriteString(fmt.Sprintf("<p><strong>Failure Reason:</strong> %s</p>\n", result.FailureReason))
		}
	}

	if result.Summary != nil {
		s := result.Summary

		sb.WriteString("<h2>Performance Summary</h2>\n")
		sb.WriteString("<table>\n")
		sb.WriteString("<tr><th>Metric</th><th>Value</th></tr>\n")
		sb.WriteString(fmt.Sprintf("<tr><td>Initial Balance</td><td>$%.2f</td></tr>\n", s.InitialBalance))
		sb.WriteString(fmt.Sprintf("<tr><td>Final Balance</td><td>$%.2f</td></tr>\n", s.FinalBalance))

		returnClass := "positive"
		if s.TotalReturn < 0 {
			returnClass = "negative"
		}
		sb.WriteString(fmt.Sprintf("<tr><td>Total Return</td><td class='%s'>$%.2f (%.2f%%)</td></tr>\n",
			returnClass, s.TotalReturn, s.TotalReturnPercent))

		sb.WriteString(fmt.Sprintf("<tr><td>Sharpe Ratio</td><td>%.2f</td></tr>\n", s.SharpeRatio))
		sb.WriteString(fmt.Sprintf("<tr><td>Max Drawdown</td><td class='negative'>$%.2f (%.2f%%)</td></tr>\n",
			s.MaxDrawdown, s.MaxDrawdownPercent))
		sb.WriteString(fmt.Sprintf("<tr><td>Win Rate</td><td>%.2f%%</td></tr>\n", s.WinRate*100))
		sb.WriteString(fmt.Sprintf("<tr><td>Total Trades</td><td>%d</td></tr>\n", s.TotalTrades))
		sb.WriteString("</table>\n")
	}

	sb.WriteString("</body></html>\n")

	return sb.String()
}

// formatMetric formats a metric for display
func formatMetric(name string, value interface{}) string {
	return fmt.Sprintf("%-25s %v\n", name+":", value)
}
