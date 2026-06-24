package main

import "fmt"

func main() {
	fmt.Println(maxProfit([]int{7, 1, 5, 3, 6, 4}))
}

func maxProfit(prices []int) int {
	buyPrice := prices[0]
	profit := 0
	for _, v := range prices {
		profit = max(profit, v-buyPrice)
		buyPrice = min(buyPrice, v)
	}
	return profit
}
