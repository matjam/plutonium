package main

import "pluto/fmt"

// Interfaces, type switches and errors. Values up to a word are stored in
// the interface; larger ones are copied into the current stack frame.

type Shape interface {
	Area() float64
	Name() string
}

type Rect struct{ W, H float64 }

func (r Rect) Area() float64 { return r.W * r.H }
func (r Rect) Name() string  { return "rect" }

type Circle struct{ R float64 }

func (c *Circle) Area() float64 { return 3.14159 * c.R * c.R }
func (c *Circle) Name() string  { return "circle" }

type NotFound struct{ Key string }

func (e *NotFound) Error() string { return "not found" }

// A Table keeps its last lookup error in its own storage, so lookup can
// return a pointer to it: &NotFound{...} would point into lookup's frame,
// which is gone once it returns.
type Table struct {
	miss NotFound
}

func (t *Table) lookup(key string) (int, error) {
	if key == "answer" {
		return 42, nil
	}
	t.miss = NotFound{Key: key}
	return 0, &t.miss
}

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		if x < 0 {
			return "negative int"
		}
		return "int"
	case string:
		return "string"
	case Shape:
		return x.Name()
	}
	return "something else"
}

func main() {
	c := Circle{R: 2}
	shapes := []Shape{Rect{3, 4}, &c}
	for _, s := range shapes {
		fmt.Printf("%s area=%.2f\n", s.Name(), s.Area())
	}
	var t Table
	for _, key := range []string{"answer", "question"} {
		v, err := t.lookup(key)
		if err != nil {
			if nf, ok := err.(*NotFound); ok {
				fmt.Printf("%s: %v\n", nf.Key, err)
			}
			continue
		}
		fmt.Printf("%s = %d\n", key, v)
	}
	fmt.Println(describe(nil), describe(7), describe(-1), describe("s"), describe(&c), describe(1.5))
}
