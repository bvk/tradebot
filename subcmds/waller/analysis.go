// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/bvk/tradebot/waller"
)

var skipVolatilityTable bool

func PrintAnalysis(a *waller.Analysis) {
	fmt.Printf("Budget required: %s\n", a.Budget().StringFixed(5))
	fmt.Printf("Fee percentage: %.6s%%\n", a.FeePct().StringFixed(5))

	fmt.Println()
	fmt.Printf("Num Buy/Sell pairs: %d\n", a.NumPairs())

	fmt.Println()
	fmt.Printf("Minimum loop fee: %s\n", a.MinLoopFee().StringFixed(5))
	fmt.Printf("Maximum loop fee: %s\n", a.MaxLoopFee().StringFixed(5))

	fmt.Println()
	fmt.Printf("Minimum price margin: %s\n", a.MinPriceMargin().StringFixed(5))
	fmt.Printf("Average price margin: %s\n", a.AvgPriceMargin().StringFixed(5))
	fmt.Printf("Maximum price margin: %s\n", a.MaxPriceMargin().StringFixed(5))

	fmt.Println()
	fmt.Printf("Minimum profit margin: %s\n", a.MinProfitMargin().StringFixed(5))
	fmt.Printf("Average profit margin: %s\n", a.AvgProfitMargin().StringFixed(5))
	fmt.Printf("Maximum profit margin: %s\n", a.MaxProfitMargin().StringFixed(5))

	if !skipVolatilityTable {
		fmt.Println()
		vols := []any{"Volatility Pct:"}
		nsells := []any{"Avg Num Sells:"}
		profits := []any{"Avg Profit:"}
		vpcts := []float64{3, 4, 5, 6, 7, 8, 9, 10, 12, 15, 20}
		for _, p := range vpcts {
			profit, sells := a.AvgProfitAtVolatility(p)
			vols = append(vols, fmt.Sprintf("%.02f%%", p))
			nsells = append(nsells, sells.StringFixed(2))
			profits = append(profits, profit.StringFixed(3))
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 1, ' ', tabwriter.AlignRight)
		fmtstr := strings.Repeat("%s\t", len(vpcts)+1)
		fmt.Fprintf(tw, fmtstr+"\n", vols...)
		fmt.Fprintf(tw, fmtstr+"\n", nsells...)
		fmt.Fprintf(tw, fmtstr+"\n", profits...)
		tw.Flush()
	}

	fmt.Println()
	for _, rate := range aprs {
		nsells := a.NumSellsForReturnRate(rate)
		fmt.Printf("For %.1f%% return\n", rate)
		fmt.Println()
		fmt.Printf("  Num sells per year:  %.2f\n", float64(nsells))
		fmt.Printf("  Num sells per month:  %.2f\n", float64(nsells)/12.0)
		fmt.Printf("  Num sells per day:  %.2f\n", float64(nsells)/365.0)
		fmt.Println()
	}
}
