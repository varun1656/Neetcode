package main

import "fmt"

func main() {
	board := [][]byte{
		{'5', '3', '.', '.', '7', '.', '.', '.', '.'},
		{'6', '.', '.', '1', '9', '5', '.', '.', '.'},
		{'.', '9', '8', '.', '.', '.', '.', '6', '.'},
		{'8', '.', '.', '.', '6', '.', '.', '.', '3'},
		{'4', '.', '.', '8', '.', '3', '.', '.', '1'},
		{'7', '.', '.', '.', '2', '.', '.', '.', '6'},
		{'.', '6', '.', '.', '.', '.', '2', '8', '.'},
		{'.', '.', '.', '4', '1', '9', '.', '.', '5'},
		{'.', '.', '.', '.', '8', '.', '.', '7', '9'},
	}
	fmt.Println(isValidSudoku(board))
}

func isValidSudoku(board [][]byte) bool {
	row := [9]map[byte]bool{}
	for i := 0; i < 9; i++ {
		row[i] = make(map[byte]bool)
	}

	col := [9]map[byte]bool{}
	for i := 0; i < 9; i++ {
		col[i] = make(map[byte]bool)
	}

	type box struct {
		row int
		col int
	}
	sq := map[box]map[byte]bool{}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			temp := box{row: i, col: j}
			sq[temp] = make(map[byte]bool)
		}
	}

	for i := 0; i < 9; i++ {
		for j := 0; j < 9; j++ {
			if board[i][j] != '.' {
				if _, exists := row[i][board[i][j]]; exists {
					return false
				} else {
					row[i][board[i][j]] = true
				}
				if _, exists := col[j][board[i][j]]; exists {
					return false
				} else {
					col[j][board[i][j]] = true
				}
				if _, exists := sq[box{row: i / 3, col: j / 3}][board[i][j]]; exists {
					return false
				} else {
					sq[box{row: i / 3, col: j / 3}][board[i][j]] = true
				}
			}
		}
	}

	return true
}
