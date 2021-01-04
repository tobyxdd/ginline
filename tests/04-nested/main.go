package main

import "fmt"

// [always_inline]
func sum(a, b int) int {
	return a + b
}

// [always_inline]
func mult(a, b int) int {
	return a * b
}

// [always_inline]
func calcs(a, b int) (x, y, z int) {
	s := sum(a, b)
	m := mult(a, b)
	return s, a - b, m
}

func main() {
	x, y, z := calcs(7, 9)
	fmt.Println(x, y, z)
}
