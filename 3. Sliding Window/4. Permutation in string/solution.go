package main

import "fmt"

func main() {
	fmt.Println(checkInclusion("abc", "eidbaooo"))
}

func checkInclusion(s1 string, s2 string) bool {
	map1 := make(map[byte]int)
	map2 := make(map[byte]int)

	for _, v := range s1 {
		map1[byte(v)]++
	}
	start, end := 0, 0
	for end < len(s2) {
		map2[s2[end]]++
		for end-start+1 > len(s1) {
			map2[s2[start]]--
			if map2[s2[start]] == 0 {
				delete(map2, s2[start])
			}
			start++
		}
		if mapEqual(map1, map2) {
			return true
		}
		end++
	}
	return false
}

func mapEqual(map1, map2 map[byte]int) bool {
	if len(map1) != len(map2) {
		return false
	}
	for k, _ := range map1 {
		if map1[k] != map2[k] {
			return false
		}
	}
	return true
}
