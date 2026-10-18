package main

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
)

// os.File is an owner (language spec sections 7.18, 18.6 and A.3): Open
// returns it by value, it closes when it is dropped, Close reports the error
// and leaves Drop nothing to do, and the zero File is not open. A closed
// descriptor number is handed out again by the next open, which shows that
// the file was closed.

// fdOf opens /dev/null in a scope of its own and returns its descriptor
// number once the file is dropped.
func fdOf() int {
	f, err := os.Open("/dev/null")
	if err != nil {
		println("open failed", err.Error())
		return -1
	}
	return f.Fd()
}

func scopeEnd() {
	println("scopeEnd")
	a := fdOf()
	b := fdOf()
	println("same descriptor again:", a == b, a > 2)
}

func earlyReturn(fail bool) error {
	f, err := os.Open("/dev/null")
	if err != nil {
		return err
	}
	if fail {
		return errors.New("failed") // f is dropped here: closed
	}
	return f.Close()
}

func early() {
	println("early")
	println(earlyReturn(true) != nil, earlyReturn(false) == nil)
	a := fdOf()
	println("descriptor reused:", a > 2)
}

func closeThenDrop() {
	println("closeThenDrop")
	f, _ := os.Open("/dev/null")
	fd := f.Fd()
	println("close:", f.Close() == nil)
	println("again:", errors.Is(f.Close(), os.ErrClosed))
	_, err := f.WriteString("x")
	println("write after close:", errors.Is(err, os.ErrClosed))
	_, err = f.Stat()
	println("stat after close:", errors.Is(err, os.ErrClosed), f.Fd())
	// The descriptor was released by Close, and dropping f closes nothing
	// again: the next open gets the same number.
	g, _ := os.Open("/dev/null")
	println("reused after Close:", g.Fd() == fd)
}

func zero() {
	println("zero")
	var f os.File
	_, err := f.Read(make1())
	println("read:", errors.Is(err, os.ErrClosed), f.Fd())
	println("close:", errors.Is(f.Close(), os.ErrClosed))
	_, err = f.Seek(0, io.SeekStart)
	println("seek:", errors.Is(err, os.ErrClosed))
	println(f.Sync() != nil, f.Truncate(0) != nil)
	// Dropping a file that was never opened does nothing.
}

var scratch [4]byte

func make1() []byte { return scratch[:] }

type Conn owner struct {
	file os.File
	name string
}

func dial(path string) (Conn, error) {
	f, err := os.Open(path)
	if err != nil {
		return Conn{}, err // f is the zero File: dropping it does nothing
	}
	var c Conn
	c.file = f
	c.name = path
	return c, nil
}

func conns() {
	println("conns")
	c, err := dial("/dev/null")
	println(err == nil, c.name)
	fd := c.file.Fd()
	{
		d, _ := dial("/dev/null")
		println("second open differs:", d.file.Fd() != fd)
	}
	e, _ := dial("/dev/null")
	println("closed with its struct:", e.file.Fd() != fd)
	_, err = dial("/nonexistent/file")
	println("error:", err != nil)
}

func array() {
	println("array")
	var files [3]os.File
	for i := range files {
		f, _ := os.Open("/dev/null")
		files[i] = f
	}
	fd := files[0].Fd()
	clear(files[:]) // closes every file that is open
	println("fd after clear:", files[0].Fd())
	g, _ := os.Open("/dev/null")
	println("lowest descriptor reused:", g.Fd() == fd)
}

func pipes() {
	println("pipes")
	r, w, err := os.Pipe()
	println(err == nil)
	w.WriteString("hello")
	var buf [16]byte
	n, _ := r.Read(buf[:])
	println(n, string1(buf[:n]))
	println("close w:", w.Close() == nil)
	_, err = r.Read(buf[:])
	println("eof:", err == io.EOF)
}

func string1(b []byte) string {
	var s string
	for _, c := range b {
		s += string(rune(c))
	}
	return s
}

func files() {
	println("files")
	dir, err := os.MkdirTemp("", "osfile-*")
	if err != nil {
		println("mkdirtemp", err.Error())
		return
	}
	name := dir + "/a.txt"
	{
		f, err := os.Create(name)
		if err != nil {
			println("create", err.Error())
			return
		}
		f.WriteString("owned")
		println("name matches:", f.Name() == name)
	} // f is dropped here: closed, and the data is on disk
	data, err := os.ReadFile(name)
	println(string1(data.Slice()), err == nil)
	data.Free()
	h, err := os.Open(name)
	fi, _ := h.Stat()
	println("size:", fi.Size())
	os.RemoveAll(dir)
}

func main() {
	scopeEnd()
	early()
	closeThenDrop()
	zero()
	conns()
	array()
	pipes()
	files()
	println("done")
}
