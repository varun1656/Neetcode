package main

import (
	"fmt"
	"strconv"
	"strings"
)

type Solution struct{}

func (s *Solution) Encode(strs []string) string {
	var sb strings.Builder
	for _, v := range strs {
		sb.WriteString(strconv.Itoa(len(v)))
		sb.WriteString("#")
		sb.WriteString(v)
	}
	return sb.String()
}

func (s *Solution) Decode(encoded string) []string {
	result := []string{}
	for i := 0; i < len(encoded); i++ {
		start := i
		end := i + 1
		for encoded[end] != '#' {
			end++
		}
		length, _ := strconv.Atoi(encoded[start:end])
		result = append(result, encoded[end+1:end+1+length])
		i = end + 1 + length - 1
	}
	return result
}

func main() {
	s := Solution{}
	encoded := s.Encode([]string{"Hello", "World"})
	decoded := s.Decode(encoded)
	fmt.Println("Encoded string: " + encoded)
	fmt.Println(decoded)
}
