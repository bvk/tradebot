// Copyright (c) 2023 BVK Chaitanya

package waller

import (
	"fmt"

	"github.com/bvk/tradebot/waller"
)

func PrintAnalysis(a *waller.Analysis) {
	fmt.Printf("Budget required: %s\n", a.Budget().StringFixed(2))
	fmt.Printf("Fee percentage: %.2f%%\n", a.FeePct())

	fmt.Println()
	fmt.Printf("Num Buy/Sell pairs: %d\n", a.NumPairs())

	fmt.Println()
	fmt.Printf("Minimum loop fee: %s\n", a.MinLoopFee().StringFixed(2))
	fmt.Printf("Maximum loop fee: %s\n", a.MaxLoopFee().StringFixed(2))

	fmt.Println()
	fmt.Printf("Minimum price margin: %s\n", a.MinPriceMargin().StringFixed(2))
	fmt.Printf("Average price margin: %s\n", a.AvgPriceMargin().StringFixed(2))
	fmt.Printf("Maximum price margin: %s\n", a.MaxPriceMargin().StringFixed(2))

	fmt.Println()
	fmt.Printf("Minimum profit margin: %s\n", a.MinProfitMargin().StringFixed(2))
	fmt.Printf("Average profit margin: %s\n", a.AvgProfitMargin().StringFixed(2))
	fmt.Printf("Maximum profit margin: %s\n", a.MaxProfitMargin().StringFixed(2))

	nsells := []int{1, 5, 10, 20, 25, 30, 40, 50, 60, 70, 75, 80, 90, 100}
	fmt.Println()
	for _, nsell := range nsells {
		rate := a.ReturnRateForNumSells(nsell)
		fmt.Printf("Return rate for %d sells per year: %s%%\n", nsell, rate.StringFixed(3))
	}
	fmt.Println()
	for _, nsell := range nsells {
		rate := a.ReturnRateForNumSells(nsell * 12)
		fmt.Printf("Return rate for %d sells per month: %s%%\n", nsell, rate.StringFixed(3))
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
