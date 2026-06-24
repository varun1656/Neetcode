package main

import "fmt"

func main() {
	fmt.Println(lengthOfLongestSubstring("abcabcbb"))
}

func lengthOfLongestSubstring(s string) int {
	start, end := 0, 0
	freq := make(map[byte]int)
	maxLen := 0
	for end < len(s) {
		freq[s[end]]++
		for freq[s[end]] > 1 {
			freq[s[start]]--
			start++
		}
		maxLen = max(maxLen, end-start+1)
		end++
	}
	return maxLen
}
