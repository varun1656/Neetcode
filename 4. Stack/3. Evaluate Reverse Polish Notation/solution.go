package main

import (
	"fmt"
	"strconv"
)

func main() {
	fmt.Println(evalRPN([]string{"2", "1", "+", "3", "*"}))
}

func evalRPN(tokens []string) int {
	stack := []int{}
	for _, v := range tokens {
		if v != "/" && v != "+" && v != "-" && v != "*" {
			i, _ := strconv.Atoi(v)
			stack = append(stack, i)
		} else {
			a1 := stack[len(stack)-1]
			a2 := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			if v == "/" {
				stack = append(stack, a2/a1)
			} else if v == "+" {
				stack = append(stack, a2+a1)
			} else if v == "-" {
				stack = append(stack, a2-a1)
			} else if v == "*" {
				stack = append(stack, a2*a1)
			}
		}
	}
	return stack[0]
}
