package lower_test

import (
	"fmt"
	"testing"
)

// TestUnionABI checks how unions pass (abi.md section 3.1): as integer
// leaves, the bytes before the payload first and then the payload's, in
// registers up to four leaves and by reference beyond; and that the slots
// holding them are tiled by their leaves, so SROA can split them.
func TestUnionABI(t *testing.T) {
	prog := lowerSource(t, `package main

type Word union {
	Ok  int
	Err int
}

type Small union {
	None
	Some uint8
}

type Half union {
	A int32
	B float32
}

type Big union {
	X [5]int64
	Y
}

//pu:noinline
func word(w Word) Word { return w }

//pu:noinline
func small(s Small) Small { return s }

//pu:noinline
func half(h Half) Half { return h }

//pu:noinline
func big(b Big) Big { return b }

func main() {
	_ = word(Word.Ok(1))
	_ = small(Small.Some(2))
	_ = half(Half.B(1.5))
	_ = big(Big.Y)
}
`)
	want := map[string]struct{ params, results string }{
		"main.word":  {"[i64 i64]", "[i64 i64]"},
		"main.small": {"[i8 i8]", "[i8 i8]"},
		"main.half":  {"[i32 i32]", "[i32 i32]"},
		"main.big":   {"[ptr ptr]", "[]"}, // a hidden result pointer, then a reference to a copy
	}
	for _, f := range prog.Funcs {
		w, ok := want[f.Name]
		if !ok {
			continue
		}
		delete(want, f.Name)
		if got := fmt.Sprint(f.Params); got != w.params {
			t.Errorf("%s: parameters %s, want %s", f.Name, got, w.params)
		}
		if got := fmt.Sprint(f.Results); got != w.results {
			t.Errorf("%s: results %s, want %s", f.Name, got, w.results)
		}
		for _, s := range f.Slots {
			if len(s.Leaves) > 0 && !s.LeavesCover() {
				t.Errorf("%s: slot %s (size %d): leaves %v and pads %v do not tile it", f.Name, s, s.Size, s.Leaves, s.Pads)
			}
		}
	}
	for name := range want {
		t.Errorf("function %s not lowered", name)
	}
}
