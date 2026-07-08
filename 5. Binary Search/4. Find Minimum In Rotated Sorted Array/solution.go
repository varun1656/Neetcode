package main

import "fmt"

func main() {
	fmt.Println(findMin([]int{2, 1}))
}

func findMin(nums []int) int {
	start, end := 0, len(nums)-1
	if nums[start] < nums[end] {
		return nums[0]
	}
	for start <= end {
		mid := start + (end-start)/2
		if mid != 0 && nums[mid-1] > nums[mid] {
			return nums[mid]
		}
		if nums[mid] < nums[end] {
			end = mid - 1
		} else {
			start = mid + 1
		}
	}
	return nums[0]
}
