package main

import "fmt"

func main() {
	fmt.Println(topKFrequent([]int{1, 2, 2, 4, 2}, 2))
}

func topKFrequent(nums []int, k int) []int {
	if len(nums) == 0 {
		return []int{}
	}
	hashmap := make(map[int]int)
	max_count := 0
	for _, v := range nums {
		hashmap[v]++
		if hashmap[v] > max_count {
			max_count = hashmap[v]
		}
	}
	freq := make([][]int, max_count+1)
	for k, v := range hashmap {
		freq[v] = append(freq[v], k)
	}
	result := []int{}
	for i := max_count; i > 0; i-- {
		for j := 0; j < len(freq[i]); j++ {
			result = append(result, freq[i][j])
			k--
			if k <= 0 {
				break
			}
		}
		if k <= 0 {
			break
		}
	}
	return result
}
