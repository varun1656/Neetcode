package main

import "fmt"

func productExceptSelf(nums []int) []int {
	if len(nums) == 0 {
		return []int{}
	}
	result := make([]int, len(nums))
	result[0] = 1
	for i := 1; i < len(nums); i++ {
		result[i] = result[i-1] * nums[i-1]
	}
	right := 1
	for i := len(nums) - 1; i >= 0; i-- {
		result[i] = result[i] * right
		right = right * nums[i]
	}
	return result
}

func main() {
	fmt.Println(productExceptSelf([]int{0, 2, 3}))
}
