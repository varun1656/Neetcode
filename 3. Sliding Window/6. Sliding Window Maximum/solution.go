package main

import (
	"fmt"
)

func main() {
	fmt.Println(maxSlidingWindow([]int{1, 3, -1, -3, 5, 3, 6, 7}, 3))
}

func maxSlidingWindow(nums []int, k int) []int {
	res := []int{}
	start, end := 0, 0
	q := []int{}
	for end < len(nums) {
		for len(q) > 0 && nums[end] > nums[q[len(q)-1]] {
			q = q[:len(q)-1]
		}
		q = append(q, end)
		for end-start+1 > k {
			start++
			for q[0] < start {
				q = q[1:]
			}
		}
		if end-start+1 == k {
			res = append(res, nums[q[0]])
		}
		end++
	}
	return res
}
