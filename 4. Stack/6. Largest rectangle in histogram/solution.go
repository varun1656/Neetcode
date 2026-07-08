package main

import "fmt"

func main() {
	fmt.Println(largestRectangleArea([]int{2, 1, 5, 6, 2, 3}))
}

func largestRectangleArea(heights []int) int {
	stack := make([]int, 0)
	Area := make([]int, len(heights))
	maxArea:=0
	for i, v := range heights {
		for len(stack) > 0 && heights[stack[len(stack)-1]] > v {
			top_index := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			Area[i]=
		}
		stack=append(stack, i)
	}
	return maxArea
}