package main

import "fmt"

func main() {
	fmt.Println(isValid("(])"))
}

func isValid(s string) bool {
	stack := []byte{}
	for i := 0; i < len(s); i++ {
		if s[i] == ')' || s[i] == ']' || s[i] == '}' {
			if len(stack) == 0 {
				return false
			} else if (s[i] == ')' && stack[len(stack)-1] == '(') || (s[i] == ']' && stack[len(stack)-1] == '[') || (s[i] == '}' && stack[len(stack)-1] == '{') {
				stack = stack[:len(stack)-1]
			} else {
				return false
			}
		} else if s[i] == '(' || s[i] == '{' || s[i] == '[' {
			stack = append(stack, s[i])
		}
	}
	if len(stack) == 0 {
		return true
	}
	return false
}
