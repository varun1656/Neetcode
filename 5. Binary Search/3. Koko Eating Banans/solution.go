package main

import (
	"fmt"
	"math"
)

func main() {
	fmt.Println(minEatingSpeed([]int{3, 6, 7, 11}, 8))
}

func minEatingSpeed(piles []int, h int) int {
	start := 1
	end := math.MinInt

	for _, v := range piles {
		end = max(end, v)
	}
	if len(piles) == h {
		return end
	}
	res := end
	for start <= end {
		mid := start + (end-start)/2
		temp := 0
		for _, v := range piles {
			hours := v / mid
			if v%mid != 0 {
				hours++
			}
			temp += hours
		}
		if temp > h {
			start = mid + 1
		} else {
			end = mid - 1
			res = min(res, mid)
		}
	}
	return res
}
