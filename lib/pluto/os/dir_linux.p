package os

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/syscall"
	"pluto/vec"
	"unsafe"
)

// A DirEntry is an entry of a directory read by ReadDir. Its name is a view
// of the DirEntries that holds it.
type DirEntry struct {
	name string
	typ  FileMode
	dir  string // the directory, for Info
	off  int    // the name's offset in DirEntries.names, until it is a view
	n    int
}

// Name returns the entry's name, a base name.
func (e *DirEntry) Name() string { return e.name }

// IsDir reports whether the entry is a directory.
func (e *DirEntry) IsDir() bool { return e.typ.IsDir() }

// Type returns the entry's type bits (FileMode.Type).
func (e *DirEntry) Type() FileMode { return e.typ }

// Info describes the entry's file, as Lstat does.
func (e *DirEntry) Info(a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	var buf [maxPath]byte
	if len(e.dir)+1+len(e.name) >= len(buf) {
		return FileInfo{}, NewPathError("lstat", e.name, syscall.ENAMETOOLONG, a)
	}
	n := copy(buf[:], e.dir)
	buf[n] = '/'
	n++
	n += copy(buf[n:], e.name)
	path := unsafe.String(&buf[0], n)
	fi, err := Lstat(path, a)
	fi.name = e.name
	return fi, err
}

// FileInfoToDirEntry returns a DirEntry for the file info describes, which
// lives in directory dir (for Info).
func FileInfoToDirEntry(dir string, info FileInfo) DirEntry {
	return DirEntry{name: info.name, typ: info.mode.Type(), dir: dir}
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
// and "..", sorted by name, in memory from a (by default the default
// allocator). A failure is a *PathError from a.
func ReadDir(name string, a alloc.Allocator = alloc.Default()) (DirEntries, error) {
	d := DirEntries{ents: vec.New[DirEntry](a), names: vec.New[byte](a)}
	var pb [maxPath]byte
	p, err := cpath(name, &pb)
	if err != nil {
		return d, NewPathError("open", name, err, a)
	}
	fd, err := syscall.Openat(syscall.AT_FDCWD, p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return d, NewPathError("open", name, err, a)
	}
	// The directory's name comes first in names, for DirEntry.Info.
	d.names.Append(unsafe.Slice(unsafe.StringData(name), len(name))...)
	var buf [8 << 10]byte
	for {
		n, err := syscall.Getdents64(fd, buf[:])
		if err != nil {
			syscall.Close(fd)
			d.Drop()
			return d, NewPathError("readdirent", name, err, a)
		}
		if n == 0 {
			break
		}
		for off := 0; off < n; {
			ent, typ, reclen := syscall.ParseDirent(buf[off:n])
			off += reclen
			if ent == "." || ent == ".." {
				continue
			}
			e := DirEntry{off: d.names.Len(), n: len(ent), typ: direntType(typ)}
			d.names.Append(unsafe.Slice(unsafe.StringData(ent), len(ent))...)
			d.ents.Push(e)
		}
	}
	syscall.Close(fd)
	// The names stop moving now: make them views.
	all := d.names.Slice()
	dir := ""
	if len(name) > 0 {
		dir = unsafe.String(&all[0], len(name))
	}
	ents := d.ents.Slice()
	for i := range ents {
		e := &ents[i]
		e.name = unsafe.String(&all[e.off], e.n)
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

// sortEntries sorts entries by name with heapsort. (pluto/sort cannot be
// used: its tests import pluto/testing, which imports this package.)
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
		if child+1 < n && e[child].name < e[child+1].name {
			child++
		}
		if !(e[root].name < e[child].name) {
			return
		}
		e[root], e[child] = e[child], e[root]
		root = child
	}
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
