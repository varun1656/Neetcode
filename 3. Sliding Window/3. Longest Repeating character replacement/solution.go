package main

import (
	"fmt"
)

func main() {
	fmt.Println(characterReplacement("ABAB", 2))
}

func characterReplacement(s string, k int) int {
	count := make([]int, 26)
	maxFreq := 0
	start := 0
	maxLength := 0

	for end := 0; end < len(s); end++ {
		// Increment the frequency of the current character
		count[s[end]-'A']++

		// Update the max frequency seen in the current window
		if count[s[end]-'A'] > maxFreq {
			maxFreq = count[s[end]-'A']
		}

		// If replacements needed > k, shrink the window
		// window length is (end - start + 1)
		for (end-start+1)-maxFreq > k {
			count[s[start]-'A']--
			start++
		}

		// Update the maximum length found so far
		if (end - start + 1) > maxLength {
			maxLength = end - start + 1
		}
	}

	return maxLength
}
