package main

// File names that are not valid UTF-8. A name on Linux is any bytes but NUL
// and '/', a string holds only valid UTF-8 (language spec section 7.3), so
// pluto/os returns a *os.NameError, holding the exact bytes, from the
// functions that have the Go name (DirEntry.Name, File.Name, FileInfo.Name,
// Readlink, Getwd, Getenv, Arg, ...), a string with U+FFFD for each invalid
// byte from the Lossy forms (NameLossy, GetwdLossy, ...), and the exact
// bytes from RawName, AppendReadlink, AppendWd, ArgBytes, GetenvBytes and
// filepath.WalkDirBytes; the Bytes functions (OpenBytes, StatBytes, ...)
// take a name as bytes, so that such a file can still be opened, renamed
// and removed. PathError.Path stays a string, lossy.

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/os/exec"
	"pluto/path/filepath"
	"pluto/strings"
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

// viewOf views the bytes of s, which the callee reads and does not change (a
// []byte(s) would be a copy that nothing owns).
func viewOf(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

// hex prints b as hexadecimal.
func hex(b []byte) {
	const digits = "0123456789abcdef"
	for _, c := range b {
		print(strings.Slice(digits, int(c>>4), int(c>>4)+1), strings.Slice(digits, int(c&15), int(c&15)+1))
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
	print("entry ", e.NameLossy(), " raw ")
	hex(e.RawName())
	name, err := e.Name()
	var ne *os.NameError
	if errors.As(err, &ne) {
		print(" error ", ne.Op, " bytes ")
		hex(ne.Bytes)
		println(" empty name", name == "")
	} else {
		println(" name", name, "valid", utf8.ValidString(name), "dir", e.IsDir())
	}
}

func main() {
	if os.NArgs() > 1 {
		child()
		return
	}
	dir, err := os.MkdirTemp("", "osnames-*-test")
	check("MkdirTemp", err)
	var pb, qb [512]byte

	// Create files through the byte API.
	for i, n := range [][]byte{nFF[:], nCut[:], nHalf[:], nE[:]} {
		f, err := os.CreateBytes(join(pb[:], dir, n))
		check("CreateBytes", err)
		f.WriteString("file ")
		f.WriteString(strings.Slice("0123", i, i+1))
		f.Close()
	}
	check("MkdirBytes", os.MkdirBytes(join(pb[:], dir, nSub[:]), 0o755))
	check("WriteFile", os.WriteFile(dir+"/plain.txt", viewOf("plain"), 0o644))

	// ReadDir: sorted by the exact bytes; Name is an error for a name that is
	// not valid UTF-8, NameLossy valid UTF-8, RawName exact.
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
		_, ferr := fi.Name()
		_, eerr := e.Name()
		println("info", fi.NameLossy() == e.NameLossy(), (ferr == nil) == (eerr == nil), fi.IsDir(), fi.IsDir() || fi.Size() > 0)
		println("info raw equal", str(fi.RawName()) == str(e.RawName()))
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

	// Open by the exact bytes; the File's Name is an error, its NameLossy
	// and the PathError's Path are lossy, its RawName exact.
	f, err := os.OpenBytes(join(pb[:], dir, nFF[:]))
	check("OpenBytes", err)
	var buf [16]byte
	n, _ := f.Read(buf[:])
	println("read", str(buf[:n]), "name valid", utf8.ValidString(f.NameLossy()))
	fname, ferr := f.Name()
	var fne *os.NameError
	println("File.Name error", errors.As(ferr, &fne), fname == "", fne != nil && fne.Op == "File.Name")
	print("File.Name bytes ")
	hexBase(fne.Bytes)
	print(" RawName ")
	hexBase(f.RawName())
	println()
	ferr = nil
	fi, err := f.Stat()
	check("File.Stat", err)
	println("file stat", fi.Size(), fi.NameLossy() == f.NameLossy())
	f.Close()
	// A file opened with a valid name has a Name and a RawName that match.
	vf, err := os.OpenBytes(join(pb[:], dir, nE[:]))
	check("OpenBytes valid", err)
	vname, verr := vf.Name()
	println("valid File.Name", verr == nil, vname == vf.NameLossy(), str(vf.RawName()) == vname)
	vf.Close()

	_, err = os.OpenBytes(join(pb[:], dir, nMiss[:]))
	println("open missing", err != nil, errors.Is(err, os.ErrNotExist))
	var pe *os.PathError
	if errors.As(err, &pe) {
		println("error path valid", utf8.ValidString(pe.Path), pe.Op)
		print("error text: ")
		println(utf8.ValidString(err.Error()))
		err = nil
	}
	// The string API for the lossy name does not find the file.
	_, err = os.Stat(dir + "/�.txt")
	println("lossy name finds nothing", errors.Is(err, os.ErrNotExist))
	err = nil

	// StatBytes and LstatBytes.
	fi, err = os.StatBytes(join(pb[:], dir, nHalf[:]))
	check("StatBytes", err)
	_, serr := fi.Name()
	println("StatBytes", fi.Size(), utf8.ValidString(fi.NameLossy()), len(fi.NameLossy()), "Name error", serr != nil)
	fi, err = os.LstatBytes(join(pb[:], dir, nCut[:]))
	check("LstatBytes", err)
	println("LstatBytes", fi.Size(), utf8.ValidString(fi.NameLossy()), fi.Mode().IsRegular())

	// ReadFileBytes and WriteFileBytes.
	check("WriteFileBytes", os.WriteFileBytes(join(pb[:], dir, nE[:]), viewOf("rewritten"), 0o600))
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
	err = nil
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
	_, werr := os.Getwd()
	var wne *os.NameError
	println("Getwd error", errors.As(werr, &wne), wne != nil && wne.Op == "Getwd")
	wd, err := os.GetwdLossy()
	check("GetwdLossy", err)
	rf := vec.NewFixed(wb[:])
	check("AppendWd 2", os.AppendWd(&rf))
	raw := rf.Slice()
	println("GetwdLossy valid", utf8.ValidString(wd), "lossy is longer", len(wd) > len(raw))
	println("NameError holds the exact directory", wne != nil && str(wne.Bytes) == str(raw))
	wne = nil
	werr = nil
	println("raw ends with y", raw[len(raw)-1] == 'y', "raw has 0xfd", rawHas(raw, 0xfd), "lossy has 0xfd", rawHas(viewOf(wd), 0xfd))
	af := vec.NewFixed(wb[:])
	check("AppendAbs", filepath.AppendAbs(&af, "z/../w"))
	abs := af.Slice()
	println("AppendAbs ends with /x/y/w", suffix(abs, "/x/y/w"), "exact", rawHas(abs, 0xfd))
	// Abs of a relative path needs the working directory as a string.
	_, err = filepath.Abs("w")
	var ane *os.NameError
	println("Abs error", errors.As(err, &ane))
	err = nil
	// Open a file relative to the directory whose name is not a string.
	check("WriteFile relative", os.WriteFile("rel.txt", viewOf("relative"), 0o644))
	check("Chdir back", os.ChdirBytes(h))
	// Readdir of the walk below shows it.

	// Symbolic links: a target that is not a string.
	c := exec.Command("sh", "-c", "ln -s \"$(printf 't\\377')\" link")
	c.Dir = dir
	check("ln", c.Run())
	c.Drop()
	_, err = os.Readlink(dir + "/link")
	var rne *os.NameError
	println("Readlink error", errors.As(err, &rne), rne != nil && rne.Op == "Readlink")
	print("Readlink error bytes ")
	hex(rne.Bytes)
	println()
	err = nil
	target, err := os.ReadlinkLossy(dir + "/link")
	check("ReadlinkLossy", err)
	println("ReadlinkLossy valid", utf8.ValidString(target), len(target))
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
	err = nil
	short := vec.NewFixed(lb[:1])
	err = os.AppendReadlink(&short, dir+"/link")
	println("target too long for the buffer", err != nil, short.Len())
	err = nil

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
	err = filepath.WalkDirBytes(viewOf(dir), func(path []byte, d *os.DirEntry, err error) error {
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
	arg0, err := os.Arg(0)
	check("Arg", err)
	println("ArgBytes(0) matches Arg(0)", str(a0) == arg0, str(a0) == os.ArgLossy(0), len(a0) > 0)
	argv, err := os.Args()
	check("Args", err)
	println("Args", argv.Len() == os.NArgs())
	argv.Free()
	println("ArgBytes out of range", os.ArgBytes(99) == nil, os.ArgBytes(-1) == nil)
	args := os.ArgsBytes()
	println("ArgsBytes", args.Len() == os.NArgs())
	args.Free()
	pathv := os.GetenvBytes(viewOf("PATH"))
	path, err := os.Getenv("PATH")
	check("Getenv", err)
	println("GetenvBytes matches Getenv", str(pathv) == path, path == os.GetenvLossy("PATH"))
	_, ok := os.LookupEnvBytes(viewOf("PLUTO_NO_SUCH_VARIABLE"))
	println("LookupEnvBytes unset", ok)
	_, ok, err = os.LookupEnv("PLUTO_NO_SUCH_VARIABLE")
	println("LookupEnv unset", ok, err == nil)
	env := os.EnvironBytes()
	envs, err := os.Environ()
	check("Environ", err)
	println("EnvironBytes", env.Len() == envs.Len())
	env.Free()
	envs.Free()
	tmp, err := os.TempDir()
	check("TempDir", err)
	println("TempDirBytes", str(os.TempDirBytes()) == tmp, tmp == os.TempDirLossy())

	// Environment values that are not valid UTF-8, in a child process: the
	// program runs itself under a shell that sets them.
	self, err := os.Arg(0)
	check("Arg self", err)
	cc := exec.Command("sh", "-c", "PLUTO_BAD=\"$(printf 'a\\377b')\" PLUTO_GOOD=ok TMPDIR=\"$1/$(printf 'sub\\200')\" exec \"$0\" child", self, dir)
	cc.Stdout = os.Stdout
	cc.Stderr = os.Stderr
	check("child", cc.Run())
	cc.Drop()

	// Remove, RemoveAll.
	check("RemoveBytes", os.RemoveBytes(join(pb[:], dir, nFE[:])))
	_, err = os.StatBytes(join(pb[:], dir, nFE[:]))
	println("removed", errors.Is(err, os.ErrNotExist))
	err = nil
	check("RemoveAllBytes", os.RemoveAllBytes(deep[:len(deep)-4]))
	_, err = os.StatBytes(deep[:len(deep)-4])
	println("RemoveAllBytes removed the tree", errors.Is(err, os.ErrNotExist))
	err = nil
	check("RemoveAll", os.RemoveAll(dir))
	_, err = os.Stat(dir)
	println("all gone", errors.Is(err, os.ErrNotExist))
	err = nil
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

// child runs in a process whose environment has a variable that is not
// valid UTF-8.
func child() {
	_, err := os.Getenv("PLUTO_BAD")
	var ne *os.NameError
	println("child Getenv error", errors.As(err, &ne), ne != nil && ne.Op == "Getenv")
	print("child Getenv bytes ")
	hex(ne.Bytes)
	println()
	println("child Error text valid", utf8.ValidString(err.Error()))
	println("child GetenvLossy", os.GetenvLossy("PLUTO_BAD"), utf8.ValidString(os.GetenvLossy("PLUTO_BAD")))
	print("child GetenvBytes ")
	hex(os.GetenvBytes(viewOf("PLUTO_BAD")))
	println()
	good, err := os.Getenv("PLUTO_GOOD")
	println("child Getenv good", good, err == nil)
	v, ok, err := os.LookupEnv("PLUTO_BAD")
	var ne2 *os.NameError
	println("child LookupEnv", v == "", ok, errors.As(err, &ne2), ne2 != nil && ne2.Op == "LookupEnv")
	lv, ok := os.LookupEnvLossy("PLUTO_BAD")
	println("child LookupEnvLossy", ok, utf8.ValidString(lv))
	_, err = os.Environ()
	var ne3 *os.NameError
	println("child Environ error", errors.As(err, &ne3), ne3 != nil && ne3.Op == "Environ")
	el := os.EnvironLossy()
	valid := true
	for _, e := range el.Slice() {
		valid = valid && utf8.ValidString(e)
	}
	println("child EnvironLossy valid", valid)
	el.Free()
	_, err = os.TempDir()
	var ne4 *os.NameError
	println("child TempDir error", errors.As(err, &ne4), ne4 != nil && ne4.Op == "TempDir")
	print("child TempDirLossy ")
	println(os.TempDirLossy() != "", utf8.ValidString(os.TempDirLossy()))
	print("child TempDirBytes ")
	hexBase(os.TempDirBytes())
	println()
	// MkdirTemp in a temporary directory that is not a string: the
	// directory is made and the error holds its path.
	_, err = os.MkdirTemp("", "x-*")
	var ne5 *os.NameError
	println("child MkdirTemp error", errors.As(err, &ne5), ne5 != nil && ne5.Op == "MkdirTemp")
	if ne5 != nil {
		println("child MkdirTemp path made", os.RemoveBytes(ne5.Bytes) == nil)
	}
}

// hexBase prints the part of b after its last slash as hexadecimal (the
// directories above vary from run to run).
func hexBase(b []byte) {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == '/' {
			b = b[i+1:]
			break
		}
	}
	hex(b)
}
