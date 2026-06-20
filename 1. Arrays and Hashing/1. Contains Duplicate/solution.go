package main

import "fmt"

func main() {
	fmt.Println(containsDuplicate([]int{1, 2, 1}))
}

func containsDuplicate(nums []int) bool {
	hashMap := make(map[int]bool)
	for i := 0; i < len(nums); i++ {
		_, exists := hashMap[nums[i]]
		if exists {
			return true
		}
		hashMap[nums[i]] = true
	}
	return false
}
