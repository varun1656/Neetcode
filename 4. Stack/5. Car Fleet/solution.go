package main

import (
	"fmt"
	"sort"
)

func main() {
	fmt.Println(carFleet(12, []int{10, 8, 0, 5, 3}, []int{2, 4, 1, 1, 3}))
}

func carFleet(target int, position []int, speed []int) int {
	cars := make([][]int, len(position))
	for i, _ := range position {
		cars[i] = make([]int, 2)
		cars[i][0] = position[i]
		cars[i][1] = speed[i]
	}
	sort.Slice(cars, func(i, j int) bool {
		return cars[i][0] > cars[j][0]
	})

	stack := make([][]int, 0)
	for _, v := range cars {
		if len(stack) == 0 {
			stack = append(stack, v)
		} else {
			stack_top_time := float64(target-stack[len(stack)-1][0]) / float64(stack[len(stack)-1][1])
			v_time := float64(target-v[0]) / float64(v[1])
			if v_time > stack_top_time {
				stack = append(stack, v)
			}
		}
	}
	return len(stack)
}
