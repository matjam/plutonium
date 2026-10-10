package filepath

import (
	"pluto/errors"
	"pluto/os"
	"unsafe"
)

// SkipDir, returned by a WalkDirFunc, skips the directory it was called
// for (or, for a file, the rest of its directory).
var SkipDir = errors.New("skip this directory")

// SkipAll, returned by a WalkDirFunc, stops the walk; WalkDir returns nil.
var SkipAll = errors.New("skip everything and stop the walk")

// A WalkDirFunc is called by WalkDir for each file and directory, as in
// Go: with err != nil when a directory could not be read (a second call
// for that directory) or root could not be stat'ed. path and d are views
// valid only during the call.
type WalkDirFunc func(path string, d *os.DirEntry, err error) error

// WalkDir walks the tree rooted at root in lexical order, calling fn for
// each file and directory, root included, as Go's WalkDir does. It does not
// follow symbolic links. Paths are built in a 4096-byte buffer; a longer
// one is reported to fn as ENAMETOOLONG.
func WalkDir(root string, fn WalkDirFunc) error {
	info, err := os.Lstat(root)
	var d os.DirEntry
	if err == nil {
		d = os.FileInfoToDirEntry(Dir(root), info)
		err = walkDir(root, &d, fn)
	} else {
		err = report(fn, root, nil, err)
	}
	if err == SkipDir || err == SkipAll {
		return nil
	}
	return err
}

func walkDir(path string, d *os.DirEntry, fn WalkDirFunc) error {
	if err := fn(path, d, nil); err != nil || !d.IsDir() {
		if err == SkipDir && d.IsDir() {
			err = nil // skip this directory
		}
		return err
	}
	ents, err := os.ReadDir(path)
	if err != nil {
		// A second call, to report the error.
		err = report(fn, path, d, err)
		if err != nil {
			if err == SkipDir && d.IsDir() {
				err = nil
			}
			return err
		}
	}
	defer ents.Drop()
	var buf [4096]byte
	for _, e := range ents.Entries() {
		if len(path)+1+len(e.Name()) > len(buf) {
			if err := report(fn, path, &e, os.NewPathError("walk", e.Name(), errNameTooLong)); err != nil {
				return err
			}
			continue
		}
		b := append(buf[:0], path...)
		if len(path) > 0 && path[len(path)-1] != '/' {
			b = append(b, '/')
		}
		b = append(b, e.Name()...)
		child := unsafe.String(unsafe.SliceData(b), len(b))
		if err := walkDir(child, &e, fn); err != nil {
			if err == SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

var errNameTooLong = errors.New("file name too long")

// report passes an allocated error to fn and frees it unless fn returns it
// (or wraps it, which the caller then frees with the wrapper).
func report(fn WalkDirFunc, path string, d *os.DirEntry, err error) error {
	r := fn(path, d, err)
	if r == nil || r == SkipDir || r == SkipAll {
		errors.Free(err)
	}
	return r
}
