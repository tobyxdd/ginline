package main

import "fmt"

// [always_inline]
func sum(a, b int, c uint32, d int) int {
	return a + b + int(c) + d
}

func main() {
	b := 2
	n := sum(1, b, 3, 4)
	fmt.Println(n)
}
