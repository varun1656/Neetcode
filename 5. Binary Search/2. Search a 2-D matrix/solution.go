package main

import "fmt"

func main() {
	fmt.Println(searchMatrix([][]int{{1, 3, 5, 7}, {10, 11, 16, 20}, {23, 30, 34, 60}}, 5))
}

func searchMatrix(matrix [][]int, target int) bool {
	start, end := 0, len(matrix)*len(matrix[0])-1
	for start <= end {
		mid := start + (end-start)/2
		row := mid / len(matrix[0])
		col := mid % len(matrix[0])
		fmt.Println(start, end, mid, row, col)
		if matrix[row][col] == target {
			return true
		}
		if matrix[row][col] < target {
			start = mid + 1
		} else {
			end = mid - 1
		}
	}
	return false
}
