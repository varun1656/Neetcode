package main

import "fmt"

func main() {
	fmt.Println(longestConsecutive([]int{100, 4, 200, 1, 3, 2}))
	fmt.Println(longestConsecutive([]int{0, 3, 7, 2, 5, 8, 4, 6, 0, 1}))
}

func longestConsecutive(nums []int) int {
	hashset := make(map[int]bool)
	for _, v := range nums {
		hashset[v] = true
	}
	result := 0
	for k, _ := range hashset {
		if hashset[k-1] == false {
			start := k
			count := 1
			start++
			for hashset[start] == true {
				start++
				count++
			}
			if count > result {
				result = count
			}
		}
	}
	return result
}
