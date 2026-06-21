package main

import (
	"fmt"
	"sort"
)

func main() {
	fmt.Println(threeSum([]int{-1, 0, 1, 2, -1, -4}))
}

func threeSum(nums []int) [][]int {
	sort.Slice(nums, func(i, j int) bool {
		return nums[i] < nums[j]
	})
	//result := make(map[[3]int]bool)
	final_result := [][]int{}
	for i := 0; i < len(nums); i++ {
		if i > 0 && nums[i] == nums[i-1] {
			continue
		}
		j, k := i+1, len(nums)-1
		for j < k {
			sum := nums[i] + nums[j] + nums[k]
			if sum == 0 {
				//if result[[3]int{nums[i], nums[j], nums[k]}] == false {
				final_result = append(final_result, []int{nums[i], nums[j], nums[k]})
				//}
				//result[[3]int{nums[i], nums[j], nums[k]}] = true
				for j < k && nums[j] == nums[j+1] {
					j++
				}
				for j < k && nums[k-1] == nums[k] {
					k--
				}
				j++
				k--
			} else if sum > 0 {
				k--
			} else {
				j++
			}
		}
	}
	return final_result
}
