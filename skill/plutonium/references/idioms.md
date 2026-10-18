# Idioms: tools, text, data

Complete short programs for common tasks. Each one is compiled and run by the skill's test, and its output is
checked. Copy one, then edit. More idioms: `idioms-types-memory.md` (owned lists and trees, enums, unions, Drop
guards, generics, constructors, multimaps), `idioms-memory.md` (arenas, packed headers, counted boxes, weak
parents) and `idioms-concurrency.md` (worker pools, pipelines, channels of owners, timers, locks, partitioned work).

| # | Task | Features |
|---|---|---|
| 1 | command-line tool with flags | `flag.NewFlagSet`, `os.Args()`, or clause |
| 2 | files: create, write, read, list lines | `os.Create`, `ReadFile`, `defer f.Close()`, `strings.Lines` |
| 3 | word frequencies | map, `vec`, `slices.SortFunc` |
| 4 | key=value config parser | `strings.Cut`, `strconv`, `or`, wrapped errors |
| 5 | formatting text | fmt verbs, `strconv`, `Builder`, `vec.Fixed` |
| 6 | an `io.Writer` | pointer-receiver interface method |
| 7 | sorting and searching | `slices.SortFunc`, `sort.SortFunc`, `BinarySearch` |
| 8 | iterators | `iter.Seq`, range over function |
| 9 | table-driven test | `pluto test`, `t.Run` |
| 10 | a module with two packages | `pluto.mod`, import path, exported names |

## 1. A command-line tool with flags

`flag.NewFlagSet` parses a slice, so the tool is testable; the real arguments come from `os.Args()` (index 0 is the program name).

```pluto-run
package main

import (
	"pluto/flag"
	"pluto/fmt"
	"pluto/os"
	"pluto/strings"
)

func run(args []string) error {
	fs := flag.NewFlagSet("greet", flag.ContinueOnError)
	name := fs.String("name", "world", "who to greet")
	times := fs.Int("n", 1, "repetitions")
	loud := fs.Bool("loud", false, "shout")
	fs.Parse(args) or return err
	greeting := "hello, " + *name
	if *loud {
		greeting = strings.ToUpper(greeting)
	}
	for i := 0; i < *times; i++ {
		fmt.Println(greeting)
	}
	fmt.Println("rest:", fs.Args())
	return nil
}

func main() int64 {
	run([]string{"-n", "2", "-loud", "-name=pluto", "extra", "args"}) or {
		fmt.Println("error:", err)
		return 2
	}
	argv := os.Args() // a vec.Vec[string]; argv[0] is the program
	run(argv.Slice()[1:]) or {
		fmt.Println("error:", err)
		return 2
	}
	return 0
}
```

```output
HELLO, PLUTO
HELLO, PLUTO
rest: [extra args]
hello, world
rest: []
```

## 2. Files: create, write, read, list lines

Files are `*os.File`; close them with `defer f.Close()`. `os.ReadFile` returns an owner (`vec.Vec[byte]`), checked into a string with `strings.FromUTF8`. A string literal is written with `WriteString` since `[]byte("...")` does not exist.

```pluto-run
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
)

func main() int64 {
	dir := os.MkdirTemp("", "skill") or return 1
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "notes.txt")

	out := os.Create(path) or return 1
	out.WriteString("alpha\nbeta\n\ngamma\n") or return 1
	fmt.Fprintf(out, "total %d\n", 4)
	out.Close() or return 1

	extra := filepath.Join(dir, "extra.txt")
	os.WriteFile(extra, strings.Bytes("more\n").Slice(), 0o644) or return 1 // WriteFile takes []byte

	data := os.ReadFile(path) or return 1
	text := strings.FromUTF8(data.Slice()) or return 1
	n := 0
	for line := range strings.Lines(text) {
		if line = strings.TrimSpace(line); line != "" {
			n++
			fmt.Println(n, line)
		}
	}

	f := os.Open(path) or return 1
	defer f.Close()
	var buf [8]byte
	size := 0
	for {
		k, err := f.Read(buf[:])
		size += k
		if err == io.EOF {
			break
		}
		if err != nil {
			return 1
		}
	}
	fmt.Println("bytes:", size)

	_, err := os.Stat(filepath.Join(dir, "missing"))
	fmt.Println(errors.Is(err, os.ErrNotExist))
	return 0
}
```

```output
1 alpha
2 beta
3 gamma
4 total 4
bytes: 26
true
```

## 3. Word frequencies with a map and sorted output

Tokenize with `strings.FieldsFuncSeq` and `unicode.IsLetter`, count in a map (a counted reference), collect the keys into a `vec.Vec`, sort the view, print. Map iteration order is unspecified.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/slices"
	"pluto/strings"
	"pluto/unicode"
	"pluto/vec"
)

func main() {
	text := "The quick brown fox -- the lazy dog; THE end!"
	counts := map[string]int{}
	notLetter := func(r rune) bool { return !unicode.IsLetter(r) }
	for w := range strings.FieldsFuncSeq(strings.ToLower(text), notLetter) {
		counts[w]++
	}
	keys := vec.WithCapacity[string](len(counts))
	for k := range counts {
		keys.Push(k)
	}
	slices.SortFunc(keys.Slice(), func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a] // most frequent first
		}
		return strings.Compare(a, b)
	})
	for i := 0; i < 3; i++ {
		fmt.Printf("%-6s %d\n", keys[i], counts[keys[i]])
	}
}
```

```output
the    3
brown  1
dog    1
```

## 4. Key=value config parsing with or clauses

`strings.Cut`, `strconv` and the or clause make a parser without nesting. Each failure names its line.

```pluto-run
package main

import (
	"pluto/errors"
	"pluto/fmt"
	"pluto/hash"
	"pluto/strconv"
	"pluto/strings"
)

var ErrSyntax = errors.New("config syntax")

type Config struct {
	port  int
	name  string
	debug bool
}

func parseLine(c *Config, line string) error {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return fmt.Errorf("%w: %q has no '='", ErrSyntax, line)
	}
	switch strings.TrimSpace(key) {
	case "port":
		c.port = strconv.Atoi(strings.TrimSpace(value)) or return fmt.Errorf("port: %w", err)
	case "name":
		c.name = strings.TrimSpace(value)
	case "debug":
		c.debug = strings.TrimSpace(value) == "true"
	default:
		return fmt.Errorf("%w: unknown key %q", ErrSyntax, key)
	}
	return nil
}

func parse(text string) (Config, error) {
	var c Config
	n := 0
	for line := range strings.Lines(text) {
		n++
		if line = strings.TrimSpace(line); line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parseLine(&c, line) or return Config{}, fmt.Errorf("line %d: %w", n, err)
	}
	return c, nil
}

func main() {
	c, err := parse("# demo\nport = 8080\nname = svc\ndebug = true\n")
	fmt.Println(c.port, c.name, c.debug, err)
	_, err = parse("port = 80\nbogus\n")
	fmt.Println(err, errors.Is(err, ErrSyntax))
	_, err = parse("port = http\n")
	fmt.Println(err, errors.Is(err, strconv.ErrSyntax))
	seen := hash.NewSet[string](4)
	fmt.Println(seen.Add("a"), seen.Add("a"), seen.Len())
}
```

```output
8080 svc true <nil>
line 2: config syntax: "bogus" has no '=' true
line 1: port: strconv.Atoi: parsing "http": invalid syntax true
true false 1
```

## 5. Formatting text: fmt verbs, strconv and Builder

`fmt` follows Go's verbs. `Sprintf` allocates a counted string; `Appendf` and `strconv.Append...` write into a `vec.Vec[byte]` or, without allocating, into a `vec.Fixed[byte]` over an array.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/strconv"
	"pluto/strings"
	"pluto/vec"
)

type Point struct {
	X, Y int
	Tag  string
}

func main() {
	p := Point{3, -4, "a"}
	fmt.Printf("%v | %+v | %d | %q\n", p, p, p.X, p.Tag)
	fmt.Printf("[%5d] [%-5d] [%05d] [%x] [%08b] [%c] [%U]\n", 42, 42, 42, 255, 5, 'G', 0x1F600)
	fmt.Printf("[%8.3f] [%e] [%g] [%t] [%s]\n", 3.14159, 1234.5, 0.000001, true, []string{"a", "b"})
	n, _ := strconv.ParseInt("-0x1f", 0, 64)
	f, _ := strconv.ParseFloat("2.5e3", 64)
	fmt.Println(n, f, strconv.Itoa(-7), strconv.FormatInt(255, 2), strconv.Quote("hi\n"))

	var b strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, "%d,", i)
	}
	fmt.Println(b.String())

	var mem [32]byte
	fixed := vec.NewFixed(mem[:]) // never allocates
	strconv.AppendInt(&fixed, 1234, 10)
	fixed.Push(' ')
	fmt.Appendf(&fixed, "%03d", 7)
	fmt.Println(len(fixed), fixed.Slice())
	s, _ := strings.FromUTF8(fixed.Slice())
	fmt.Println(s)
}
```

```output
{3 -4 a} | {X:3 Y:-4 Tag:a} | 3 | "a"
[   42] [42   ] [00042] [ff] [00000101] [G] [U+1F600]
[   3.142] [1.234500e+03] [1e-06] [true] [[a b]]
-31 2500 -7 11111111 "hi\n"
0,1,2,
8 [49 50 51 52 32 48 48 55]
1234 007
```

## 6. Implementing io.Writer

Interfaces are satisfied by pointer-receiver methods; convert with `&x`. `fmt.Fprintf` formats straight into any `io.Writer` without allocating.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
)

type Upper struct {
	w io.Writer
	n int
}

func (u *Upper) Write(p []byte) (int, error) {
	var buf [64]byte
	total := 0
	for len(p) > 0 {
		k := min(len(p), len(buf))
		for i := 0; i < k; i++ {
			c := p[i]
			if c >= 'a' && c <= 'z' {
				c -= 'a' - 'A'
			}
			buf[i] = c
		}
		w, err := u.w.Write(buf[:k])
		total += w
		if err != nil {
			return total, err
		}
		p = p[k:]
	}
	u.n += total
	return total, nil
}

func main() {
	up := Upper{w: os.Stdout}
	fmt.Fprintf(&up, "hello %s, you are %d\n", "pluto", 3)
	io.WriteString(&up, "bytes written are counted\n")
	fmt.Println(up.n)
}
```

```output
HELLO PLUTO, YOU ARE 3
BYTES WRITTEN ARE COUNTED
49
```

## 7. Sorting and searching

`slices.SortFunc` takes a three-way comparison; `sort.SortFunc` takes a less function. `slices.BinarySearch` needs sorted input.

```pluto-run
package main

import (
	"pluto/cmp"
	"pluto/fmt"
	"pluto/slices"
	"pluto/sort"
)

type Person struct {
	Name string
	Age  int
}

func main() {
	people := []Person{{"Ana", 31}, {"Bob", 25}, {"Cy", 31}, {"Di", 22}}
	slices.SortFunc(people, func(a, b Person) int {
		if c := cmp.Compare(b.Age, a.Age); c != 0 { // oldest first
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	fmt.Println(people)
	names := []string{"pear", "fig", "apple"}
	sort.Strings(names)
	i, found := slices.BinarySearch(names, "fig")
	fmt.Println(names, i, found)
	sort.SortFunc(people, func(a, b Person) bool { return a.Name < b.Name })
	fmt.Println(people[0].Name, slices.IsSortedFunc(people, func(a, b Person) int { return cmp.Compare(a.Name, b.Name) }))
}
```

```output
[{Ana 31} {Cy 31} {Bob 25} {Di 22}]
[apple fig pear] 1 true
Ana true
```

## 8. Iterators: range over functions

A function `func(yield func(V) bool)` is an iterator; `iter.Seq[V]` names it. Break out of the loop and `yield` returns false. Library containers range over their `All` method.

```pluto-run
package main

import (
	"pluto/fmt"
	"pluto/iter"
	"pluto/slices"
)

func Fib() iter.Seq[int] {
	return func(yield func(int) bool) {
		a, b := 0, 1
		for yield(a) {
			a, b = b, a+b
		}
	}
}

func Take[V any](s iter.Seq[V], n int) iter.Seq[V] {
	return func(yield func(V) bool) {
		if n <= 0 {
			return
		}
		i := 0
		for v := range s {
			if !yield(v) {
				return
			}
			i++
			if i == n {
				return
			}
		}
	}
}

func Evens(s iter.Seq[int]) iter.Seq[int] {
	return func(yield func(int) bool) {
		for v := range s {
			if v%2 == 0 && !yield(v) {
				return
			}
		}
	}
}

func Pairs(xs []string) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		for i, x := range xs {
			if !yield(i, x) {
				return
			}
		}
	}
}

func main() {
	for v := range Take(Evens(Fib()), 5) {
		fmt.Print(v, " ")
	}
	fmt.Println()
	for i, s := range Pairs([]string{"a", "b"}) {
		fmt.Print(i, s, " ")
	}
	fmt.Println()
	v := slices.Collect(Take(Fib(), 8)) // a vec.Vec
	fmt.Println(v)
}
```

```output
0 2 8 34 144 
0a 1b 
[0 1 1 2 3 5 8 13]
```

## 9. A table-driven test

A test is a `_test.p` file in the package under test, with `func TestXxx(t *testing.T)`. Run it with `pluto test`; `-run` and `-v` work as in Go.

```pluto-test
//file: mathx.p
package mathx

func Clamp(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

//file: mathx_test.p
package mathx

import "pluto/testing"

func TestClamp(t *testing.T) {
	tests := []struct {
		name      string
		x, lo, hi int
		want      int
	}{
		{"below", -5, 0, 10, 0},
		{"inside", 5, 0, 10, 5},
		{"above", 50, 0, 10, 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Clamp(tc.x, tc.lo, tc.hi); got != tc.want {
				t.Errorf("Clamp(%d, %d, %d) = %d, want %d", tc.x, tc.lo, tc.hi, got, tc.want)
			}
		})
	}
}

func TestAbs(t *testing.T) {
	if Abs(-3) != 3 || Abs(3) != 3 {
		t.Fatal("Abs is wrong")
	}
}
```


## 10. A module with two packages

A directory with a `pluto.mod` whose first line is `module example.com/shapes` is a module. Its packages are imported by
module path plus directory, exported names start with a capital letter, and there are no downloads (E4008 for any
path outside the module and `pluto/`). Build the module with `pluto build .` (or `pluto run .`) in its root; test one
package with `pluto test ./geom`.

```pluto-run
//file: pluto.mod
module example.com/shapes

//file: geom/geom.p
package geom

type Rect struct{ W, H float64 }

func New(w, h float64) Rect { return Rect{w, h} }

func (r Rect) Area() float64 { return r.W * r.H }

//file: main.p
package main

import (
	"example.com/shapes/geom"
	"pluto/fmt"
)

func main() {
	r := geom.New(2, 3)
	fmt.Println(r.Area(), r)
}
```

```output
6 {2 3}
```
