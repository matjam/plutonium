package token

import (
	"fmt"
	"sort"
	"sync"
)

// Pos is a compact encoding of a source position. It can be converted
// into a Position for a more convenient but larger representation.
//
// The zero value for Pos is NoPos; there is no file and line information
// associated with it, and NoPos.IsValid() is false.
type Pos int

// NoPos is the zero value for Pos; there is no file and line information
// associated with it, and NoPos.IsValid() is false. NoPos is always
// smaller than any other Pos value. The corresponding Position value
// for NoPos is the zero value for Position.
const NoPos Pos = 0

// IsValid reports whether the position is valid.
func (p Pos) IsValid() bool {
	return p != NoPos
}

// Position describes an arbitrary source position including the file,
// line, and column location. A Position is valid if the line number is > 0.
type Position struct {
	Filename string // filename, if any
	Offset   int    // offset, starting at 0
	Line     int    // line number, starting at 1
	Column   int    // column number, starting at 1 (byte count)
}

// IsValid reports whether the position is valid.
func (pos Position) IsValid() bool {
	return pos.Line > 0
}

// String returns a string in one of several forms:
//
//	file:line:column    valid position with file name
//	file:line           valid position with file name but no column (column == 0)
//	line:column         valid position without file name
//	line                valid position without file name and no column (column == 0)
//	-                   invalid position
func (pos Position) String() string {
	s := pos.Filename
	if pos.IsValid() {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprintf("%d", pos.Line)
		if pos.Column != 0 {
			s += fmt.Sprintf(":%d", pos.Column)
		}
	}
	if s == "" {
		s = "-"
	}
	return s
}

// File is a handle for a file belonging to a FileSet.
// A File has a name, size, and line offset table.
type File struct {
	name string
	base int // Pos value range for this file is [base, base+size]
	size int

	mu    sync.Mutex
	lines []int // lines[i] is the offset of the first character of line i+1
}

// Name returns the file name of file f as registered with AddFile.
func (f *File) Name() string {
	return f.name
}

// Base returns the base offset of file f as registered with AddFile.
func (f *File) Base() int {
	return f.base
}

// Size returns the size of file f as registered with AddFile.
func (f *File) Size() int {
	return f.size
}

// LineCount returns the number of lines in file f.
func (f *File) LineCount() int {
	f.mu.Lock()
	n := len(f.lines)
	f.mu.Unlock()
	return n
}

// AddLine adds the line offset for a new line.
// The line offset must be larger than the offset for the previous line
// and smaller than the file size; otherwise the line offset is ignored.
func (f *File) AddLine(offset int) {
	f.mu.Lock()
	if i := len(f.lines); (i == 0 || f.lines[i-1] < offset) && offset < f.size {
		f.lines = append(f.lines, offset)
	}
	f.mu.Unlock()
}

// Pos returns the Pos value for the given file offset;
// the offset must be <= f.Size().
// f.Pos(f.Offset(p)) == p.
func (f *File) Pos(offset int) Pos {
	if offset > f.size {
		panic(fmt.Sprintf("invalid file offset %d (file size is %d)", offset, f.size))
	}
	return Pos(f.base + offset)
}

// Offset returns the offset for the given file position p;
// p must be a valid Pos value in that file.
func (f *File) Offset(p Pos) int {
	if int(p) < f.base || int(p) > f.base+f.size {
		panic(fmt.Sprintf("invalid Pos value %d (range [%d, %d])", p, f.base, f.base+f.size))
	}
	return int(p) - f.base
}

// lineFor returns the 1-based line number for the given file offset.
func (f *File) lineFor(offset int) int {
	f.mu.Lock()
	lines := f.lines
	f.mu.Unlock()

	// Binary search to find which line the offset belongs to.
	// lines[i] is the offset of the start of line i+1 (0-indexed).
	i := sort.Search(len(lines), func(i int) bool { return lines[i] > offset }) - 1
	if i < 0 {
		i = 0
	}
	return i + 1 // convert to 1-based
}

// columnFor returns the 1-based column number for the given file offset.
func (f *File) columnFor(offset int) int {
	f.mu.Lock()
	lines := f.lines
	f.mu.Unlock()

	// Find the line start offset.
	i := sort.Search(len(lines), func(i int) bool { return lines[i] > offset }) - 1
	if i < 0 {
		i = 0
	}
	lineStart := lines[i]
	return offset - lineStart + 1
}

// Position returns the Position value for the given Pos value in this file.
func (f *File) Position(p Pos) Position {
	offset := f.Offset(p)
	return Position{
		Filename: f.name,
		Offset:   offset,
		Line:     f.lineFor(offset),
		Column:   f.columnFor(offset),
	}
}

// FileSet represents a set of source files. Methods of file sets are
// safe for concurrent use.
type FileSet struct {
	mu    sync.RWMutex
	base  int     // base offset for the next file
	files []*File // list of files in the order added to the set
}

// NewFileSet creates a new file set.
func NewFileSet() *FileSet {
	return &FileSet{
		base: 1, // 0 == NoPos
	}
}

// AddFile adds a new file with a given filename, base, and file size
// to the file set s and returns the file. Multiple files may have the
// same name. The base offset must not be smaller than the FileSet's
// base; and size must not be negative. As a special case, if a negative
// base is provided, the current value of the FileSet's base is used
// instead. Adding the file incrementally increases the FileSet's base
// value by size + 1.
func (s *FileSet) AddFile(filename string, base, size int) *File {
	s.mu.Lock()
	defer s.mu.Unlock()

	if base < 0 {
		base = s.base
	}
	if base < s.base {
		panic(fmt.Sprintf("invalid base %d (should be >= %d)", base, s.base))
	}
	if size < 0 {
		panic(fmt.Sprintf("invalid size %d (should be >= 0)", size))
	}

	f := &File{
		name:  filename,
		base:  base,
		size:  size,
		lines: []int{0}, // line 1 starts at offset 0
	}
	base += size + 1 // +1 so that EOF also has a valid Pos
	if base < 0 {
		panic("token.FileSet: file base out of range")
	}
	s.base = base
	s.files = append(s.files, f)
	return f
}

// File returns the file that contains the position p.
// If no such file is found (for instance for p == NoPos),
// the result is nil.
func (s *FileSet) File(p Pos) *File {
	if p == NoPos {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, f := range s.files {
		if Pos(f.base) <= p && p <= Pos(f.base+f.size) {
			return f
		}
	}
	return nil
}

// Position converts a Pos p in the fileset into a Position value.
// If adjusted is set, the position may be adjusted by position-altering
// //line comments; otherwise those comments are ignored.
// p must be a Pos value in s or NoPos.
func (s *FileSet) Position(p Pos) Position {
	if p == NoPos {
		return Position{}
	}
	f := s.File(p)
	if f == nil {
		return Position{}
	}
	return f.Position(p)
}
