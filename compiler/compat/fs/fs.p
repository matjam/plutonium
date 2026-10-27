// Package fs is the part of Go's io/fs that code translated by tools/go2pu
// uses: the interfaces of a read-only file system (FS, DirEntry, FileInfo),
// the functions ReadDir, ReadFile and Stat that read through one, the
// sentinel errors, and DirFS, the file system of a directory of the
// operating system. Go's FS has one method, Open, and the functions fall
// back to it for the rest; this FS lists the three it reads with, which is
// all the compiler's loader needs, so that no File interface and no
// readers are involved.
//
// Everything allocates from alloc.Default() and never frees, as the package
// compat does.
package fs

import (
	"compiler/compat"
	"compiler/pu"
	"pluto/os"
)

// ErrNotExist and ErrInvalid are Go's fs.ErrNotExist and fs.ErrInvalid:
// the errors of a file that does not exist and of an invalid argument.
var (
	ErrNotExist = os.ErrNotExist
	ErrInvalid  = os.ErrInvalid
)

// A FileMode is the type and permission bits of a file.
type FileMode = os.FileMode

// A FileInfo describes a file.
type FileInfo interface {
	Name() string
	Size() int64
	Mode() FileMode
	IsDir() bool
}

// A DirEntry is an entry of a directory.
type DirEntry interface {
	Name() string
	IsDir() bool
	Type() FileMode
	Info() (FileInfo, error)
}

// An FS is a read-only file system, with slash-separated paths relative to
// its root.
type FS interface {
	ReadDir(name string) ([]DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Stat(name string) (FileInfo, error)
}

// ReadDir reads the named directory of fsys and returns its entries sorted
// by file name.
func ReadDir(fsys FS, name string) ([]DirEntry, error) { return fsys.ReadDir(name) }

// ReadFile reads the named file of fsys and returns its contents.
func ReadFile(fsys FS, name string) ([]byte, error) { return fsys.ReadFile(name) }

// Stat returns a description of the named file of fsys.
func Stat(fsys FS, name string) (FileInfo, error) { return fsys.Stat(name) }

// DirFS returns the file system of the directory dir.
func DirFS(dir string) FS { return pu.New(dirFS{dir: dir}) }

// OSReadDir is os.ReadDir: the entries of the named directory, sorted by
// file name.
func OSReadDir(name string) ([]DirEntry, error) { return DirFS("").ReadDir(name) }

// OSStat is os.Stat: a description of the named file.
func OSStat(name string) (FileInfo, error) { return DirFS("").Stat(name) }

// A dirFS is the file system of a directory; with no directory its names are
// operating system paths.
type dirFS struct{ dir string }

// path returns the operating system path of the file name of d.
func (d *dirFS) path(name string) string {
	if d.dir == "" {
		return name
	}
	if name == "." || name == "" {
		return d.dir
	}
	return compat.Concat(d.dir, "/", name)
}

func (d *dirFS) ReadDir(name string) ([]DirEntry, error) {
	dir := d.path(name)
	ents := os.ReadDir(dir) or return nil, err
	out := compat.Make[DirEntry](ents.Len())
	for i := 0; i < ents.Len(); i++ {
		e := ents.At(i)
		out[i] = pu.New(dirEntry{name: compat.Concat(e.NameLossy()), typ: e.Type(), dir: dir})
	}
	return out, nil
}

func (d *dirFS) ReadFile(name string) ([]byte, error) {
	return compat.ReadFile(d.path(name))
}

func (d *dirFS) Stat(name string) (FileInfo, error) {
	info := os.Stat(d.path(name)) or return nil, err
	return pu.New(fileInfo{name: compat.Concat(info.NameLossy()), size: info.Size(), mode: info.Mode()}), nil
}

type dirEntry struct {
	name string
	typ  FileMode
	dir  string
}

func (e *dirEntry) Name() string   { return e.name }
func (e *dirEntry) IsDir() bool    { return e.typ.IsDir() }
func (e *dirEntry) Type() FileMode { return e.typ.Type() }

func (e *dirEntry) Info() (FileInfo, error) {
	info := os.Stat(compat.Concat(e.dir, "/", e.name)) or return nil, err
	return pu.New(fileInfo{name: compat.Concat(info.NameLossy()), size: info.Size(), mode: info.Mode()}), nil
}

type fileInfo struct {
	name string
	size int64
	mode FileMode
}

func (i *fileInfo) Name() string   { return i.name }
func (i *fileInfo) Size() int64    { return i.size }
func (i *fileInfo) Mode() FileMode { return i.mode }
func (i *fileInfo) IsDir() bool    { return i.mode.IsDir() }
