# ginline

Go AST inliner

## Example

### Source

```go
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
```

### Generated

```go
package main

import "fmt"

func main() {
	var (
		a int
		b int
		c int
	)
	{
		var (
			_rv_0 int
			_rv_1 int
			_rv_2 int
		)
		{
			var (
				a int = 7
				b int = 9
			)
			{
				_rv_0 = a + b
				_rv_1 = a - b
				_rv_2 = a * b
			}
		}
		a, b, c = _rv_0, _rv_1, _rv_2
	}
	fmt.Println(a, b, c)
}

```

## Limitations

- Only supports calls without using return values (`foo()`) and simple assignments (`a, b, c := foo()`). The cases where it appears in expressions (`bar(foo()+1)`) are not supported for now **(WIP)**
- Methods (`func (b Bar) Foo()`) are not supported for now **(WIP)**
- Can't handle `defer` correctly (Unsure about the solution. Suggestions are welcome)
