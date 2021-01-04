package main

import "fmt"

// [always_inline]
func calcs(a, b int) (int, int, int) {
	return a + b, a - b, a * b
}

func main() {
	a, b, c := calcs(7, 9)
	fmt.Println(a, b, c)
}
