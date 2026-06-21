package main

import (
	"fmt"
	"strings"
)

func main() {
	fmt.Println(isPalindrome("A man, a plan, a canal: Panama"))
}

func isAlphanumeric(a byte) bool {
	return (a >= 'a' && a <= 'z') || (a >= '0' && a <= '9')
}

func isPalindrome(s string) bool {
	s = strings.ToLower(s)
	start, end := 0, len(s)-1
	for start <= end {
		for start <= end && !isAlphanumeric(s[start]) {
			start++
		}
		for start <= end && !isAlphanumeric(s[end]) {
			end--
		}
		if start <= end && s[start] != s[end] {
			return false
		}
		start++
		end--
	}
	return true
}
