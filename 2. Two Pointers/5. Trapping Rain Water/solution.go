package main

import "fmt"

func main() {
	fmt.Println(trap([]int{0, 1, 0, 2, 1, 0, 1, 3, 2, 1, 2, 1}))
}

func trap(height []int) int {
	max_left := 0
	max_right := 0
	i, j := 0, len(height)-1
	waterTrap := 0
	for i < j {
		if height[i] < height[j] {
			waterTrap += calculateWaterTrap(height[i], max_left)
			max_left = max(height[i], max_left)
			i++
		} else {
			waterTrap += calculateWaterTrap(height[j], max_right)
			max_right = max(height[j], max_right)
			j--
		}
	}
	return waterTrap
}
func calculateWaterTrap(h, boundary int) int {
	if h > boundary {
		return 0
	}
	return boundary - h
}
