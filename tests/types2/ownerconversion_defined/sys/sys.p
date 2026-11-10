package sys

type Fd owner int32

func (f *Fd) Drop() {}

func Open() Fd { return Fd(3) } // allowed: Fd's own package

type Other owner int32

// Conversions that make a sys owner are allowed in sys.
func Moves(fd Fd) Other { return Other(fd) } // rule 3 in the target's package

func Forms(fd *Fd, n *int32) (*int32, *Fd) {
	return (*int32)(fd), (*Fd)(n) // pointer conversions both ways
}

type Slot owner union {
	Empty
	Full Fd
}

type Slot2 owner union {
	Empty
	Full Fd
}

func Swap(s Slot) Slot2 { return Slot2(s) } // owner union in its package

// Region's fields are all exported: its literal is public.
type Region owner struct {
	Base, Len uintptr
}

// File has an unexported field: only sys makes one.
type File owner struct {
	Name string
	fd   int32
}

func NewFile() File { return File{Name: "a", fd: 3} }

type Inner owner Fd
