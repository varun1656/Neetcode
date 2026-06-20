package main

import "fmt"

func main() {
	fmt.Println(isAnagram("anagram", "aaagram"))
}

func isAnagram(s string, t string) bool {
	if len(s) != len(t) {
		return false
	}
	hashmap1 := make(map[rune]int)

	for _, r := range s {
		hashmap1[r]++
	}
	for _, r := range t {
		hashmap1[r]--
		if hashmap1[r] < 0 {
			return false
		}
	}

	return true
}
