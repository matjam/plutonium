package os

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/rt"
	"pluto/syscall"
	"pluto/vec"
	"unsafe"
)

// A DirEntry is an entry of a directory read by ReadDir. Its names are views
// of the DirEntries that holds it.
//
// A file name is any bytes but NUL and '/', so it need not be valid UTF-8,
// which a string must be (language spec section 6.10). RawName returns the
// exact bytes. Name returns a string: the name itself if it is valid UTF-8,
// and otherwise a copy of it with U+FFFD in place of each invalid byte.
// Two different files can have the same Name that way; open them by RawName
// with the Bytes functions (OpenBytes, StatBytes, ...).
type DirEntry struct {
	name string // the name as a valid string
	raw  []byte // the exact name
	typ  FileMode
	dir  []byte // the directory's exact path, for Info
	off  int    // the name's offset in DirEntries.names, until it is a view
	n    int
	loff int // the offset of the valid form of the name (off if it is valid)
	ln   int
}

// Name returns the entry's name, a base name, as a valid UTF-8 string: with
// U+FFFD for each invalid byte if the name has any. RawName returns the
// exact bytes.
func (e *DirEntry) Name() string { return e.name }

// RawName returns the exact bytes of the entry's name, a view valid until
// the DirEntries that holds it is dropped. Do not modify it.
func (e *DirEntry) RawName() []byte { return e.raw }

// IsDir reports whether the entry is a directory.
func (e *DirEntry) IsDir() bool { return e.typ.IsDir() }

// Type returns the entry's type bits (FileMode.Type).
func (e *DirEntry) Type() FileMode { return e.typ }

// Info describes the entry's file, as Lstat does. The FileInfo's Name is
// the entry's Name.
func (e *DirEntry) Info(a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	var buf [maxPath]byte
	if len(e.dir)+1+len(e.raw) >= len(buf) {
		return FileInfo{}, NewPathErrorBytes("lstat", e.raw, syscall.ENAMETOOLONG, a)
	}
	n := copy(buf[:], e.dir)
	buf[n] = '/'
	n++
	n += copy(buf[n:], e.raw)
	fi, err := LstatBytes(buf[:n], a)
	fi.name = e.name
	fi.raw = e.raw
	return fi, err
}

// FileInfoToDirEntry returns a DirEntry for the file info describes, which
// lives in directory dir (for Info).
func FileInfoToDirEntry(dir string, info FileInfo) DirEntry {
	return FileInfoToDirEntryBytes(strBytes(dir), info)
}

// FileInfoToDirEntryBytes is FileInfoToDirEntry for a directory that is
// bytes. The entry views dir and the FileInfo's name, which must outlive
// it.
func FileInfoToDirEntryBytes(dir []byte, info FileInfo) DirEntry {
	return DirEntry{name: info.name, raw: info.raw, typ: info.mode.Type(), dir: dir}
}

// DirEntries is the result of ReadDir: the entries of a directory sorted by
// name, and the memory their names live in. It owns both, and its entries
// are valid until it is dropped. Declaring it owner is a one-line change
// once the keyword lands (roadmap item 51).
type DirEntries struct {
	ents  vec.Vec[DirEntry]
	names vec.Vec[byte]
}

// Entries returns the entries, a view valid until d is dropped.
func (d *DirEntries) Entries() []DirEntry { return d.ents.Slice() }

// Len returns the number of entries.
func (d *DirEntries) Len() int { return d.ents.Len() }

// At returns a pointer to entry i, valid until d is dropped.
func (d *DirEntries) At(i int) *DirEntry { return d.ents.Ptr(i) }

// Drop frees the entries and their names.
func (d *DirEntries) Drop() {
	d.ents.Free()
	d.names.Free()
}

// ReadDir reads the named directory and returns its entries, without "."
// and "..", sorted by name (by the exact bytes of the names), in memory
// from a (by default the default allocator). A failure is a *PathError
// from a. Names that are not valid UTF-8 are made valid for Name and kept
// exact for RawName (see DirEntry).
func ReadDir(name string, a alloc.Allocator = alloc.Default()) (DirEntries, error) {
	return readDir(strBytes(name), name, a)
}

// ReadDirBytes is ReadDir for a directory name that is bytes (see
// OpenBytes).
func ReadDirBytes(name []byte, a alloc.Allocator = alloc.Default()) (DirEntries, error) {
	return readDir(name, "", a)
}

func readDir(name []byte, sname string, a alloc.Allocator) (DirEntries, error) {
	d := DirEntries{ents: vec.New[DirEntry](a), names: vec.New[byte](a)}
	var pb [maxPath]byte
	p, err := cpathBytes(name, &pb)
	if err != nil {
		return d, newPathError("open", name, sname, err, a)
	}
	fd, err := syscall.Openat(syscall.AT_FDCWD, p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return d, newPathError("open", name, sname, err, a)
	}
	// The directory's name comes first in names, for DirEntry.Info.
	d.names.Append(name...)
	var buf [8 << 10]byte
	for {
		n, err := syscall.Getdents64(fd, buf[:])
		if err != nil {
			syscall.Close(fd)
			d.Drop()
			return d, newPathError("readdirent", name, sname, err, a)
		}
		if n == 0 {
			break
		}
		for off := 0; off < n; {
			ent, typ, reclen := syscall.ParseDirent(buf[off:n])
			off += reclen
			if isDots(ent) {
				continue
			}
			e := DirEntry{off: d.names.Len(), n: len(ent), typ: direntType(typ)}
			d.names.Append(ent...)
			e.loff, e.ln = e.off, e.n
			if len(ent) > 0 {
				if valid, _ := rt.ValidUTF8(&ent[0], len(ent)); !valid {
					// Keep the name as a valid string next to the exact
					// bytes: the same bytes with U+FFFD for each invalid
					// one. (A copy from a, appended and then dropped.)
					fixed := rt.StringFromBytes(&ent[0], len(ent), a)
					e.loff, e.ln = d.names.Len(), len(fixed)
					d.names.Append(strBytes(fixed)...)
				}
			}
			d.ents.Push(e)
		}
	}
	syscall.Close(fd)
	// The names stop moving now: make them views.
	all := d.names.Slice()
	var dir []byte
	if len(name) > 0 {
		dir = all[:len(name):len(name)]
	}
	ents := d.ents.Slice()
	for i := range ents {
		e := &ents[i]
		if e.n > 0 {
			e.raw = all[e.off : e.off+e.n : e.off+e.n]
			// Valid UTF-8: either the exact bytes, checked above, or the
			// copy StringFromBytes made.
			e.name = unsafe.String(&all[e.loff], e.ln)
		}
		e.dir = dir
		if e.typ == ModeIrregular {
			// The file system did not say: ask.
			if fi, err := e.Info(a); err == nil {
				e.typ = fi.mode.Type()
			} else {
				errors.Free(err)
			}
		}
	}
	sortEntries(ents)
	return d, nil
}

// sortEntries sorts entries by name (the exact bytes) with heapsort.
// (pluto/sort cannot be used: its tests import pluto/testing, which imports
// this package.)
func sortEntries(e []DirEntry) {
	n := len(e)
	for i := n/2 - 1; i >= 0; i-- {
		siftDown(e, i, n)
	}
	for end := n - 1; end > 0; end-- {
		e[0], e[end] = e[end], e[0]
		siftDown(e, 0, end)
	}
}

func siftDown(e []DirEntry, root, n int) {
	for {
		child := 2*root + 1
		if child >= n {
			return
		}
		if child+1 < n && bytesLess(e[child].raw, e[child+1].raw) {
			child++
		}
		if !bytesLess(e[root].raw, e[child].raw) {
			return
		}
		e[root], e[child] = e[child], e[root]
		root = child
	}
}

// bytesLess reports whether a sorts before b, comparing bytes.
func bytesLess(a, b []byte) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// direntType converts a getdents type to FileMode type bits; unknown
// types come back as ModeIrregular for ReadDir to look up.
func direntType(t uint8) FileMode {
	switch t {
	case syscall.DT_REG:
		return 0
	case syscall.DT_DIR:
		return ModeDir
	case syscall.DT_LNK:
		return ModeSymlink
	case syscall.DT_FIFO:
		return ModeNamedPipe
	case syscall.DT_SOCK:
		return ModeSocket
	case syscall.DT_CHR:
		return ModeDevice | ModeCharDevice
	case syscall.DT_BLK:
		return ModeDevice
	}
	return ModeIrregular
}
