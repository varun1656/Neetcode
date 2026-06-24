package main

import (
	"fmt"
	"math"
)

func main() {
	fmt.Println(minWindow("ADOBECODEBANC", "ABC"))
}

func minWindow(s string, t string) string {
	if len(s) < len(t) {
		return ""
	}
	mapT := make(map[byte]int)
	mapS := make(map[byte]int)
	for _, v := range t {
		mapT[byte(v)]++
	}
	start, end := 0, 0
	needed := len(mapT)
	haveit := 0
	res := ""
	res_len := math.MaxInt
	for end < len(s) {
		mapS[s[end]]++
		if mapS[s[end]] == mapT[s[end]] {
			haveit++
		}

		for start <= end && haveit == needed {
			if haveit == needed && end-start+1 < res_len {
				res_len = end - start + 1
				res = s[start : end+1]
			}
			mapS[s[start]]--
			if _, exists := mapT[s[start]]; exists && mapS[s[start]] < mapT[s[start]] {
				haveit--
			}
			start++
		}
		end++
	}
	return res
}
