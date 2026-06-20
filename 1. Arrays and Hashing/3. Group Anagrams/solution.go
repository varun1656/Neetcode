package main

import "fmt"

func main() {
	strs := []string{"eat", "tea", "tan", "ate", "nat", "bat"}
	fmt.Println(groupAnagrams(strs))
}

func groupAnagrams(strs []string) [][]string {
	hashmap := make(map[[26]int][]string)
	for _, s := range strs {
		key1 := [26]int{}
		for i := 0; i < len(s); i++ {
			key1[s[i]-'a']++
		}
		hashmap[key1] = append(hashmap[key1], s)
	}
	result := [][]string{}
	for _, val := range hashmap {
		result = append(result, val)
	}
	return result
}
