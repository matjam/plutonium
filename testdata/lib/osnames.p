package main

// File names that are not valid UTF-8. A name on Linux is any bytes but NUL
// and '/', a string holds only valid UTF-8 (language spec section 6.10), so
// pluto/os hands names back as strings with U+FFFD for each invalid byte
// (DirEntry.Name, Readlink, Getwd, PathError, filepath.WalkDir) and keeps
// the exact bytes next to them (DirEntry.RawName, AppendReadlink,
// AppendWd, ArgBytes, filepath.WalkDirBytes); the Bytes functions
// (OpenBytes, StatBytes, ...) take a name as bytes, so that such a file can
// still be opened, renamed and removed.

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/os/exec"
	"pluto/path/filepath"
	"pluto/unicode/utf8"
	"pluto/vec"
	"unsafe"
)

func check(what string, err error) {
	if err != nil {
		println(what, "failed:", err.Error())
		os.Exit(1)
	}
}

func str(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

// hex prints b as hexadecimal.
func hex(b []byte) {
	const digits = "0123456789abcdef"
	for _, c := range b {
		print(digits[c>>4:c>>4+1], digits[c&15:c&15+1])
	}
}

// join returns dir/name in buf.
func join(buf []byte, dir string, name []byte) []byte {
	b := vec.NewFixed(buf)
	vec.AppendString(&b, dir)
	b.Push('/')
	b.Append(name...)
	return b.Slice()
}

// The names: none is a string, since none of the invalid ones can be.
var (
	nFF   = [...]byte{0xff, '.', 't', 'x', 't'}            // an invalid byte
	nCut  = [...]byte{0xc3}                                // a rune cut short
	nHalf = [...]byte{'a', 0xe2, 0x82, '.', 't', 'x', 't'} // a three-byte rune cut short
	nE    = [...]byte{0xc3, 0xa9, '.', 't', 'x', 't'}      // "é.txt", valid
	nFE   = [...]byte{0xfe, '.', 't', 'x', 't'}
	nDir  = [...]byte{'d', 0xfd, 'r'}
	nSub  = [...]byte{'s', 'u', 'b', 0x80}
	nMiss = [...]byte{'n', 'o', 0xf8, 'p', 'e'}
)

func show(e *os.DirEntry) {
	print("entry ", e.Name(), " raw ")
	hex(e.RawName())
	println(" valid", utf8.ValidString(e.Name()), "dir", e.IsDir())
}

func main() {
	dir, err := os.MkdirTemp("", "osnames-*-test")
	check("MkdirTemp", err)
	var pb, qb [512]byte

	// Create files through the byte API.
	for i, n := range [][]byte{nFF[:], nCut[:], nHalf[:], nE[:]} {
		f, err := os.CreateBytes(join(pb[:], dir, n))
		check("CreateBytes", err)
		f.WriteString("file ")
		f.WriteString("0123"[i : i+1])
		f.Close()
	}
	check("MkdirBytes", os.MkdirBytes(join(pb[:], dir, nSub[:]), 0o755))
	check("WriteFile", os.WriteFile(dir+"/plain.txt", []byte("plain"), 0o644))

	// ReadDir: sorted by the exact bytes; Name is valid UTF-8, RawName exact.
	ents, err := os.ReadDir(dir)
	check("ReadDir", err)
	println("entries", ents.Len())
	for i := 0; i < ents.Len(); i++ {
		show(ents.At(i))
	}
	// Info keeps working for the invalid names, as it opens them by RawName.
	for i := 0; i < ents.Len(); i++ {
		e := ents.At(i)
		fi, err := e.Info()
		check("Info", err)
		println("info", fi.Name() == e.Name(), fi.IsDir(), fi.IsDir() || fi.Size() > 0)
	}
	ents.Drop()

	// ReadDirBytes of a directory with a name that is not a string.
	sub := join(pb[:], dir, nSub[:])
	g, err := os.CreateBytes(join(qb[:], str(sub), nFF[:]))
	check("Create in sub", err)
	g.Close()
	ents, err = os.ReadDirBytes(sub)
	check("ReadDirBytes", err)
	println("sub entries", ents.Len())
	for i := 0; i < ents.Len(); i++ {
		e := ents.At(i)
		show(e)
		fi, err := e.Info()
		check("Info in sub", err)
		println("sub info", fi.Size())
	}
	ents.Drop()

	// Open by the exact bytes; the File's name and the error are lossy.
	f, err := os.OpenBytes(join(pb[:], dir, nFF[:]))
	check("OpenBytes", err)
	var buf [16]byte
	n, _ := f.Read(buf[:])
	println("read", str(buf[:n]), "name valid", utf8.ValidString(f.Name()))
	fi, err := f.Stat()
	check("File.Stat", err)
	println("file stat", fi.Size(), fi.Name() == f.Name())
	f.Close()

	_, err = os.OpenBytes(join(pb[:], dir, nMiss[:]))
	println("open missing", err != nil, errors.Is(err, os.ErrNotExist))
	if pe, ok := err.(*os.PathError); ok {
		println("error path valid", utf8.ValidString(pe.Path), pe.Op)
		print("error text: ")
		println(utf8.ValidString(err.Error()))
		errors.Free(err)
	}
	// The string API for the lossy name does not find the file.
	_, err = os.Stat(dir + "/�.txt")
	println("lossy name finds nothing", errors.Is(err, os.ErrNotExist))
	errors.Free(err)

	// StatBytes and LstatBytes.
	fi, err = os.StatBytes(join(pb[:], dir, nHalf[:]))
	check("StatBytes", err)
	println("StatBytes", fi.Size(), utf8.ValidString(fi.Name()), len(fi.Name()))
	fi, err = os.LstatBytes(join(pb[:], dir, nCut[:]))
	check("LstatBytes", err)
	println("LstatBytes", fi.Size(), utf8.ValidString(fi.Name()), fi.Mode().IsRegular())

	// ReadFileBytes and WriteFileBytes.
	check("WriteFileBytes", os.WriteFileBytes(join(pb[:], dir, nE[:]), []byte("rewritten"), 0o600))
	data, err := os.ReadFileBytes(join(pb[:], dir, nE[:]))
	check("ReadFileBytes", err)
	println("ReadFileBytes", str(data.Slice()))
	data.Free()
	check("ChmodBytes", os.ChmodBytes(join(pb[:], dir, nE[:]), 0o640))
	fi, err = os.StatBytes(join(pb[:], dir, nE[:]))
	check("Stat after chmod", err)
	println("mode", fi.Mode().Perm().String())

	// RenameBytes.
	check("RenameBytes", os.RenameBytes(join(pb[:], dir, nFF[:]), join(qb[:], dir, nFE[:])))
	_, err = os.StatBytes(join(pb[:], dir, nFF[:]))
	println("old name gone", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	_, err = os.StatBytes(join(pb[:], dir, nFE[:]))
	println("new name there", err == nil)

	// MkdirAllBytes and ChdirBytes, Getwd and AppendWd, filepath.Abs.
	df := vec.NewFixed(pb[:])
	df.Append(join(qb[:], dir, nDir[:])...)
	vec.AppendString(&df, "/x/y")
	deep := df.Slice()
	check("MkdirAllBytes", os.MkdirAllBytes(deep, 0o755))
	var wb, home [4096]byte
	hf := vec.NewFixed(home[:])
	check("AppendWd", os.AppendWd(&hf))
	h := hf.Slice()
	check("ChdirBytes", os.ChdirBytes(deep))
	wd, err := os.Getwd()
	check("Getwd", err)
	rf := vec.NewFixed(wb[:])
	check("AppendWd 2", os.AppendWd(&rf))
	raw := rf.Slice()
	println("Getwd valid", utf8.ValidString(wd), "lossy is longer", len(wd) > len(raw))
	println("raw ends with y", raw[len(raw)-1] == 'y', "raw has 0xfd", rawHas(raw, 0xfd), "lossy has 0xfd", rawHas([]byte(wd), 0xfd))
	af := vec.NewFixed(wb[:])
	check("AppendAbs", filepath.AppendAbs(&af, "z/../w"))
	abs := af.Slice()
	println("AppendAbs ends with /x/y/w", suffix(abs, "/x/y/w"), "exact", rawHas(abs, 0xfd))
	s, err := filepath.Abs("w")
	check("Abs", err)
	println("Abs valid", utf8.ValidString(s), "ends with /x/y/w", suffix([]byte(s), "/x/y/w"))
	// Open a file relative to the directory whose name is not a string.
	check("WriteFile relative", os.WriteFile("rel.txt", []byte("relative"), 0o644))
	check("Chdir back", os.ChdirBytes(h))
	// Readdir of the walk below shows it.

	// Symbolic links: a target that is not a string.
	c := exec.Command("sh", "-c", "ln -s \"$(printf 't\\377')\" link")
	c.Dir = dir
	check("ln", c.Run())
	c.Drop()
	target, err := os.Readlink(dir + "/link")
	check("Readlink", err)
	println("Readlink valid", utf8.ValidString(target), len(target))
	var lb [64]byte
	rawt := vec.NewFixed(lb[:])
	check("AppendReadlink", os.AppendReadlink(&rawt, dir+"/link"))
	print("AppendReadlink ")
	hex(rawt.Slice())
	println()
	rawt.Reset()
	lp := vec.NewFixed(pb[:])
	vec.AppendString(&lp, dir)
	vec.AppendString(&lp, "/link")
	check("ReadlinkBytes", os.ReadlinkBytes(&rawt, lp.Slice()))
	print("ReadlinkBytes ")
	hex(rawt.Slice())
	println()
	_, err = os.Readlink(dir + "/plain.txt")
	println("Readlink of a file fails", err != nil)
	errors.Free(err)
	short := vec.NewFixed(lb[:1])
	err = os.AppendReadlink(&short, dir+"/link")
	println("target too long for the buffer", err != nil, short.Len())
	errors.Free(err)

	// Walking: WalkDir gives valid paths, WalkDirBytes the exact ones, which
	// can be opened.
	nstr := 0
	err = filepath.WalkDir(dir, func(path string, d *os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !utf8.ValidString(path) {
			println("INVALID path")
		}
		nstr++
		return nil
	})
	check("WalkDir", err)
	println("WalkDir visited", nstr)
	nraw, opened := 0, 0
	err = filepath.WalkDirBytes([]byte(dir), func(path []byte, d *os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		nraw++
		if !d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			if f, err := os.OpenBytes(path); err == nil {
				opened++
				f.Close()
			} else {
				print("cannot open ")
				hex(path)
				println()
			}
		}
		return nil
	})
	check("WalkDirBytes", err)
	println("WalkDirBytes visited", nraw, "opened", opened)

	// Command-line arguments and the environment.
	a0 := os.ArgBytes(0)
	println("ArgBytes(0) matches Arg(0)", str(a0) == os.Arg(0), len(a0) > 0)
	println("ArgBytes out of range", os.ArgBytes(99) == nil, os.ArgBytes(-1) == nil)
	args := os.ArgsBytes()
	println("ArgsBytes", args.Len() == os.NArgs())
	args.Free()
	pathv := os.GetenvBytes([]byte("PATH"))
	println("GetenvBytes matches Getenv", str(pathv) == os.Getenv("PATH"))
	_, ok := os.LookupEnvBytes([]byte("PLUTO_NO_SUCH_VARIABLE"))
	println("LookupEnvBytes unset", ok)
	env := os.EnvironBytes()
	envs := os.Environ()
	println("EnvironBytes", env.Len() == envs.Len())
	env.Free()
	envs.Free()
	println("TempDirBytes", str(os.TempDirBytes()) == os.TempDir())

	// Remove, RemoveAll.
	check("RemoveBytes", os.RemoveBytes(join(pb[:], dir, nFE[:])))
	_, err = os.StatBytes(join(pb[:], dir, nFE[:]))
	println("removed", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	check("RemoveAllBytes", os.RemoveAllBytes(deep[:len(deep)-4]))
	_, err = os.StatBytes(deep[:len(deep)-4])
	println("RemoveAllBytes removed the tree", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	check("RemoveAll", os.RemoveAll(dir))
	_, err = os.Stat(dir)
	println("all gone", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	_ = io.EOF
}

func rawHas(b []byte, c byte) bool {
	for _, x := range b {
		if x == c {
			return true
		}
	}
	return false
}

func suffix(b []byte, s string) bool {
	return len(b) >= len(s) && str(b[len(b)-len(s):]) == s
}
