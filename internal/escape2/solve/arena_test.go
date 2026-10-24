package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape2"
)

// Local arenas (section 18.9, eir/arena.go): what is allocated from an
// arena in a local variable is a view of the arena. Each program is
// rejected at exactly the lines of reject (main.p:LINE), or accepted.
var arenaCases = []struct {
	name, src string
	reject    []string
}{
	{
		// The window of defer alloc.Use(&a).Restore() runs to the end of the
		// function: text made in it is a view of a (arena_default).
		"window_deferred", `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)

func name(n int) string {
	a := arena.NewFixed(256)
	defer alloc.Use(&a).Restore()
	return fmt.Sprintf("item %d", n)
}

func main() { println(name(1)) }
`, []string{"main.p:12:"},
	},
	{
		// A window ends with its block: text made after it is not of the
		// arena.
		"window_block", `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)

func name(n int) string {
	a := arena.NewFixed(256)
	{
		r := alloc.Use(&a)
		println(len(fmt.Sprintf("in %d", n)))
		r.Restore()
	}
	return fmt.Sprintf("item %d", n)
}

func main() { println(name(1)) }
`, nil,
	},
	{
		// An arena the caller passes: what is allocated from it is the
		// caller's to keep, until the caller's own arena is gone.
		"window_caller_arena", `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)

func name(a *arena.Fixed, n int) string {
	defer alloc.Use(a).Restore()
	return fmt.Sprintf("item %d", n)
}

func keep() int {
	a := arena.NewFixed(256)
	return len(name(&a, 2))
}

func leak() string {
	a := arena.NewFixed(256)
	return name(&a, 3)
}

func main() {
	println(keep())
	println(leak())
}
`, []string{"main.p:21:"},
	},
	{
		// An allocator that outlives the frame changes nothing.
		"window_heap_allocator", `package main

import (
	"pluto/alloc"
	"pluto/fmt"
)

func name(n int) string {
	h := alloc.Default()
	defer alloc.Use(h).Restore()
	return fmt.Sprintf("item %d", n)
}

func main() { println(name(1)) }
`, nil,
	},
	{
		// A range-over-func body runs within its loop's window.
		"window_range_body", `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)

func seq(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

var saved string

func f() {
	a := arena.NewFixed(1024)
	defer alloc.Use(&a).Restore()
	for i := range seq {
		saved = fmt.Sprintf("%d", i)
	}
}

func main() { f() }
`, []string{"main.p:22:"},
	},
	{
		// A defaulted allocator parameter left out in a window takes the
		// arena: vec.New[T]() (arena_default).
		"window_default_param", `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/vec"
)

func items() vec.Vec[int] {
	a := arena.NewFixed(1024)
	defer alloc.Use(&a).Restore()
	v := vec.New[int]()
	v.Push(1)
	return v
}

func main() { println(items().Len()) }
`, []string{"main.p:14:"},
	},
	{
		// The Restore holds the arena (//pu:resultholds a): it may not
		// outlive it.
		"restore_returned", `package main

import (
	"pluto/alloc"
	"pluto/arena"
)

var keep alloc.Restore

func f() {
	a := arena.NewFixed(256)
	keep = alloc.Use(&a)
}

func main() { f() }
`, []string{"main.p:12:"},
	},
	{
		// Text a Builder started on a local arena returns is a view of it
		// (arena_boxed); its bytes may be written.
		"explicit_builder", `package main

import (
	"pluto/arena"
	"pluto/os"
	"pluto/strings"
)

func leak() string {
	a := arena.NewFixed(1024)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello")
	return out.String()
}

func fine() int {
	a := arena.NewFixed(1024)
	var out strings.Builder
	out.Init(&a)
	out.WriteString("hello")
	os.Stdout.Write(out.Bytes())
	return len(out.String())
}

func main() {
	println(len(leak()))
	println(fine())
}
`, []string{"main.p:14:"},
	},
	{
		// Converting arena text to an interface copies it into a frame box
		// released when the function returns, after the arena is gone
		// (E3313, arena_boxed).
		"box_copy", `package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)

func window(n int) int {
	a := arena.NewFixed(1024)
	defer alloc.Use(&a).Restore()
	s := fmt.Sprintf("n=%d", n)
	var x any = s
	_ = x
	return len(s)
}

func main() { println(window(1)) }
`, []string{"main.p:13:"},
	},
	{
		// A map made from a local arena holds it, and is a view of it.
		"map_arena", `package main

import "pluto/arena"

var keep map[int]int

func local() int {
	ar := arena.NewFixed(1 << 12)
	m := make(map[int]int, 0, &ar)
	m[1] = 2
	return m[1]
}

func kept() {
	ar := arena.NewFixed(1 << 12)
	m := make(map[int]int, 0, &ar)
	m[1] = 2
	keep = m
}

func main() {
	println(local())
	kept()
}
`, []string{"main.p:18:"},
	},
}

func TestArenas(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the library")
	}
	for _, c := range arenaCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "main.p")
			if err := os.WriteFile(f, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			prog := loadFile(t, f)
			err := escape2.Check(prog)
			var lines []string
			if err != nil {
				for _, l := range strings.Split(err.Error(), "\n") {
					if i := strings.Index(l, "main.p:"); i >= 0 && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
						rest := l[i:]
						if j := strings.IndexByte(rest[len("main.p:"):], ':'); j >= 0 {
							lines = append(lines, rest[:len("main.p:")+j+1])
						}
					}
				}
			}
			got := strings.Join(dedupe(lines), " ")
			want := strings.Join(c.reject, " ")
			if got != want {
				t.Errorf("rejected at %q, want %q:\n%v", got, want, err)
			}
		})
	}
}

func dedupe(ss []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
