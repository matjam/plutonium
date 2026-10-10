package main

// The file system layer of pluto/os: temporary directories, whole-file
// reads and writes, Stat, ReadDir, MkdirAll, Rename, Chmod, Remove,
// RemoveAll, the working directory and the errors each reports.

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/strings"
	"pluto/vec"
	"unsafe"
)

func check(what string, err error) {
	if err != nil {
		println(what, "failed:", err.Error())
		os.Exit(1)
	}
}

// join builds dir/name in buf and returns it as a view.
func join(buf []byte, dir, name string) string {
	b := vec.NewFixed(buf)
	vec.AppendString(&b, dir)
	b.Push('/')
	vec.AppendString(&b, name)
	return unsafe.String(unsafe.SliceData(buf), b.Len())
}

func bytesOf(s string) []byte { return unsafe.Slice(unsafe.StringData(s), len(s)) }

func str(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

func main() {
	dir, err := os.MkdirTemp("", "osfs-*-test")
	check("MkdirTemp", err)
	println("temp has prefix", strings.HasPrefix(dir, os.TempDir()), strings.HasSuffix(dir, "-test"))
	fi, err := os.Stat(dir)
	check("Stat dir", err)
	var mb [16]byte
	println("dir mode", fi.Mode().Perm().String(), fi.IsDir())
	mf := vec.NewFixed(mb[:])
	fi.Mode().AppendString(&mf)
	println("dir mode string", str(mf.Slice()))

	// WriteFile, ReadFile and Stat.
	var pb, qb, rb [512]byte
	a := join(pb[:], dir, "a.txt")
	check("WriteFile", os.WriteFile(a, bytesOf("hello, files\n"), 0o640))
	data, err := os.ReadFile(a)
	check("ReadFile", err)
	print("read: ", unsafe.String(unsafe.SliceData(data.Slice()), data.Len()))
	data.Free()
	fi, err = os.Stat(a)
	check("Stat", err)
	println("stat", fi.Name(), fi.Size(), fi.IsDir(), fi.Mode().IsRegular(), uint32(fi.Mode().Perm()) == 0o640, fi.ModTime().Year() >= 2024)

	// A bigger file, read through io.ReadAll.
	f, err := os.Create(join(qb[:], dir, "big.bin"))
	check("Create", err)
	var block [1000]byte
	for i := range block {
		block[i] = byte(i)
	}
	for i := 0; i < 100; i++ {
		_, err := f.Write(block[:])
		check("Write", err)
	}
	off, err := f.Seek(0, io.SeekCurrent)
	check("Seek", err)
	check("Close", f.Close())
	f, err = os.Open(join(qb[:], dir, "big.bin"))
	check("Open", err)
	all, err := io.ReadAll(f)
	check("ReadAll", err)
	println("big", off, all.Len(), all.At(999), all.At(99999))
	all.Free()
	ffi, err := f.Stat()
	check("File.Stat", err)
	println("file stat", ffi.Name(), ffi.Size())
	f.Close()

	// MkdirAll, ReadDir and DirEntry.
	var nb [512]byte
	nested := join(nb[:], dir, "x/y/z")
	check("MkdirAll", os.MkdirAll(nested, 0o755))
	check("MkdirAll again", os.MkdirAll(nested, 0o755))
	check("WriteFile in z", os.WriteFile(join(rb[:], nested, "deep.txt"), bytesOf("deep"), 0o600))
	check("Mkdir c", os.Mkdir(join(rb[:], dir, "c"), 0o755))
	ents, err := os.ReadDir(dir)
	check("ReadDir", err)
	for _, e := range ents.Entries() {
		println("entry", e.Name(), e.IsDir())
	}
	info, err := ents.At(0).Info()
	check("Info", err)
	println("info", info.Name(), info.Size())
	ents.Drop()

	// Rename and Chmod.
	b := join(qb[:], dir, "b.txt")
	check("Rename", os.Rename(a, b))
	_, err = os.Stat(a)
	println("old name gone", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	check("Chmod", os.Chmod(b, 0o600))
	fi, err = os.Lstat(b)
	check("Lstat", err)
	println("chmod", uint32(fi.Mode().Perm()) == 0o600)

	// Errors carry the operation and the path.
	err = os.Mkdir(b, 0o755)
	var pe *os.PathError
	println("mkdir over file", errors.As(err, &pe), pe.Op, errors.Is(err, os.ErrExist), pe.Path == b)
	errors.Free(err)
	err = os.Remove(join(rb[:], dir, "missing"))
	println("remove missing", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	err = os.Remove(join(rb[:], dir, "x"))
	println("remove non-empty dir fails", err != nil)
	errors.Free(err)
	_, err = os.ReadDir(b)
	println("readdir of a file", errors.Is(err, os.ErrNotExist), err != nil)
	errors.Free(err)

	// The working directory.
	old, err := os.Getwd()
	check("Getwd", err)
	check("Chdir", os.Chdir(nested))
	var wb [4096]byte
	cwdb := vec.NewFixed(wb[:])
	check("Getwd 2", os.AppendWd(&cwdb))
	cwd := str(cwdb.Slice())
	println("chdir", strings.HasSuffix(cwd, "/x/y/z"))
	data, err = os.ReadFile("deep.txt")
	check("relative ReadFile", err)
	println("relative read", data.Len())
	data.Free()
	check("Chdir back", os.Chdir(old))

	// Remove and RemoveAll.
	check("Remove file", os.Remove(b))
	check("Remove empty dir", os.Remove(join(rb[:], dir, "c")))
	check("RemoveAll", os.RemoveAll(dir))
	_, err = os.Stat(dir)
	println("all gone", errors.Is(err, os.ErrNotExist))
	errors.Free(err)
	check("RemoveAll missing", os.RemoveAll(dir))

	// Environment and process.
	v, ok := os.LookupEnv("PLUTO_SURELY_UNSET_VARIABLE")
	println("lookup unset", v == "", ok)
	env := os.Environ()
	println("environ", env.Len() > 0, os.Getpid() > 0)
	env.Free()
	args := os.Args()
	println("args", args.Len(), args.At(0) == os.Arg(0))
	args.Free()
}
