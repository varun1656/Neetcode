package main

import "fmt"

func main() {
	fmt.Println(twoSum([]int{2, 7, 11, 15}, 9))
}

func twoSum(numbers []int, target int) []int {
	start, end := 0, len(numbers)-1
	for start <= end {
		sum := numbers[start] + numbers[end]
		if sum == target {
			return []int{start + 1, end + 1}
		} else if sum < target {
			start++
		} else {
			end--
		}
	}
	return []int{}
}
