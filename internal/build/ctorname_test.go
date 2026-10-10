package build_test

import (
	"debug/dwarf"
	"debug/elf"
	"path/filepath"
	"testing"

	"github.com/matjam/plutonium/internal/build"
)

// TestCtorClosureNames checks that the closure a closure constructor builds
// is named after the constructor, not the function that calls it, in the
// symbol table and in DWARF, and that the calls of one constructor share the
// closure's code: one symbol each for Count and for each instance of Bag's
// All, none named after main or after the other callers.
func TestCtorClosureNames(t *testing.T) {
	const src = `package main

type Bag[T any] struct{ items []T }

func (b *Bag[T]) All() func(yield func(int, T) bool) {
	return func(yield func(int, T) bool) {
		for i, x := range b.items {
			if !yield(i, x) {
				return
			}
		}
	}
}

func Count(n int) func(yield func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

//pu:noinline
func other() int {
	t := 0
	for i := range Count(3) {
		t += i
	}
	return t
}

func main() {
	t := 0
	for i := range Count(4) {
		t += i
	}
	b := &Bag[int]{items: []int{1, 2}}
	for _, x := range b.All() {
		t += x
	}
	s := &Bag[string]{items: []string{"a"}}
	for range s.All() {
		t++
	}
	println(t, other())
}
`
	for _, level := range []int{0, 2} {
		t.Run("O"+string(rune('0'+level)), func(t *testing.T) {
			dir, _, _ := buildFiles(t, map[string][]byte{"main.p": []byte(src)}, build.Config{OptLevel: level})
			f, err := elf.Open(filepath.Join(dir, "prog"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			syms, err := f.Symbols()
			if err != nil {
				t.Fatal(err)
			}
			count := map[string]int{}
			for _, s := range syms {
				if elf.ST_TYPE(s.Info) == elf.STT_FUNC {
					count[s.Name]++
				}
			}
			d, err := f.DWARF()
			if err != nil {
				t.Fatal(err)
			}
			subs := map[string]bool{}
			r := d.Reader()
			for {
				e, err := r.Next()
				if err != nil {
					t.Fatal(err)
				}
				if e == nil {
					break
				}
				if e.Tag == dwarf.TagSubprogram {
					name, _ := e.Val(dwarf.AttrName).(string)
					subs[name] = true
				}
			}
			want := []string{"main.Count.func1", "main.(*Bag).All[int].func1", "main.(*Bag).All[string].func1"}
			for _, name := range want {
				if count[name] != 1 {
					t.Errorf("symbol %s: %d, want 1", name, count[name])
				}
				if !subs[name] {
					t.Errorf("no DWARF subprogram %s", name)
				}
			}
			for _, name := range []string{"main.main.func1", "main.other.func1"} {
				if count[name] != 0 || subs[name] {
					t.Errorf("closure named after its caller: %s", name)
				}
			}
		})
	}
}
