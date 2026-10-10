package filepath

import (
	"pluto/errors"
	"pluto/os"
	"pluto/unicode/utf8"
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
//
// A file name need not be valid UTF-8, but a string must be, so path has
// each invalid byte replaced by U+FFFD (language spec section 6.10), and so
// has d.Name(). Such a path cannot be opened; d.RawName() and
// WalkDirBytes give the exact bytes.
type WalkDirFunc func(path string, d *os.DirEntry, err error) error

// A WalkDirBytesFunc is a WalkDirFunc whose path is the exact bytes of the
// path, a view valid only during the call.
type WalkDirBytesFunc func(path []byte, d *os.DirEntry, err error) error

// walker holds the function a walk calls: fs for WalkDir, fb for
// WalkDirBytes.
type walker struct {
	fs WalkDirFunc
	fb WalkDirBytesFunc
}

// call passes path to the function.
func (w *walker) call(path []byte, d *os.DirEntry, err error) error {
	if w.fb != nil {
		return w.fb(path, d, err)
	}
	if utf8.Valid(path) {
		// Checked just now; the view lasts for the call, as documented.
		return w.fs(unsafe.String(unsafe.SliceData(path), len(path)), d, err)
	}
	return w.fs(string(path), d, err) // a copy, with U+FFFD for invalid bytes
}

// WalkDir walks the tree rooted at root in lexical order, calling fn for
// each file and directory, root included, as Go's WalkDir does. It does not
// follow symbolic links. Paths are built in a 4096-byte buffer; a longer
// one is reported to fn as ENAMETOOLONG.
func WalkDir(root string, fn WalkDirFunc) error {
	w := walker{fs: fn}
	dir := Dir(root)
	return walkRoot(&w, strBytes(root), strBytes(dir))
}

// WalkDirBytes is WalkDir for a root that is bytes, calling fn with the
// exact bytes of each path, so that it walks trees whose names are not
// valid UTF-8 and the paths can be opened with the Bytes functions of
// pluto/os.
func WalkDirBytes(root []byte, fn WalkDirBytesFunc) error {
	w := walker{fb: fn}
	return walkRoot(&w, root, dirBytes(root))
}

// walkRoot walks the tree at root, whose directory part is dir.
func walkRoot(w *walker, root []byte, dir []byte) error {
	info, err := os.LstatBytes(root)
	var d os.DirEntry
	if err == nil {
		d = os.FileInfoToDirEntryBytes(dir, info)
		err = walkDir(w, root, &d)
	} else {
		err = w.report(root, nil, err)
	}
	if err == SkipDir || err == SkipAll {
		return nil
	}
	return err
}

// dirBytes returns the directory part of path, as Dir does, without
// cleaning it.
func dirBytes(path []byte) []byte {
	i := len(path) - 1
	for i >= 0 && path[i] != '/' {
		i--
	}
	if i < 0 {
		return strBytes(".")
	}
	if i == 0 {
		return path[:1]
	}
	return path[:i]
}

func walkDir(w *walker, path []byte, d *os.DirEntry) error {
	if err := w.call(path, d, nil); err != nil || !d.IsDir() {
		if err == SkipDir && d.IsDir() {
			err = nil // skip this directory
		}
		return err
	}
	ents, err := os.ReadDirBytes(path)
	if err != nil {
		// A second call, to report the error.
		err = w.report(path, d, err)
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
		raw := e.RawName()
		if len(path)+1+len(raw) > len(buf) {
			if err := w.report(path, &e, os.NewPathErrorBytes("walk", raw, errNameTooLong)); err != nil {
				return err
			}
			continue
		}
		child := append(buf[:0], path...)
		if len(path) > 0 && path[len(path)-1] != '/' {
			child = append(child, '/')
		}
		child = append(child, raw...)
		if err := walkDir(w, child, &e); err != nil {
			if err == SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

var errNameTooLong = errors.New("file name too long")

// report passes an allocated error to the function and frees it unless it
// returns it (or wraps it, which the caller then frees with the wrapper).
func (w *walker) report(path []byte, d *os.DirEntry, err error) error {
	r := w.call(path, d, err)
	if r == nil || r == SkipDir || r == SkipAll {
		errors.Free(err)
	}
	return r
}
