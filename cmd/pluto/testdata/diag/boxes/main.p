package main

// owned, shared, synced and weak in the type system (language spec
// sections 5.6 and 6.5).

type Reader interface{ Read(p []byte) int }

// A box of an interface: an owning interface does that job.
var r owned[Reader]

type Node struct{ val int }

// weak refers through a counted reference, not to the object itself.
var w weak[Node]

type Buf owner struct{ data []byte }

// A value receiver consumes the owner.
func (b Buf) Finish() string { return "" }

type Finisher interface{ Finish() string }

func finish(b Buf) {
	var f Finisher = b
	_ = f
}

func main() {}
