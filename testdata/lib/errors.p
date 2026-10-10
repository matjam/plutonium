// Error handling conventions: sentinels, Errno, os.PathError, an error kept
// in its owner's storage, wrapping across layers with fmt.Errorf, Join,
// errors.Is/As/AsType/Unwrap/Free, fmt.ErrorBuf, and the io and strconv
// sentinels.
//
// errors.out was checked against Go running this file with each pluto/...
// import pointed at a shim over Go's package. The lines that differ are
// Plutonium behaviour and were checked by hand: PathError copies the path
// (Go's keeps the caller's string, which this program overwrites), the
// allocation counts (Go's errors do not go through pluto/alloc), and %w
// outside Errorf, which prints %!w(error) like the other bad verbs.
package main

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/fmt"
	"pluto/io"
	"pluto/os"
	"pluto/strconv"
	"pluto/strings"
	"pluto/syscall"
	"pluto/vec"
	"unsafe"
)

// Sentinel errors live in static storage.
var (
	ErrEmpty   = errors.New("empty input")
	ErrTooDeep = errors.New("nesting too deep")
)

// ----------------------------------------------------------------------------
// A parser that keeps its error in its own storage.

type ParseError struct {
	Line, Col int
	Err       error // the sentinel describing what went wrong
	text      [64]byte
	n         int
}

func (e *ParseError) Error() string { return unsafe.String(&e.text[0], e.n) }
func (e *ParseError) Unwrap() error { return e.Err }

type Parser struct {
	src       string
	line, col int
	depth     int
	err       ParseError
}

// fail records the error in the parser and returns a pointer to it.
func (p *Parser) fail(err error) error {
	p.err.Line, p.err.Col, p.err.Err = p.line, p.col, err
	f := vec.NewFixed(p.err.text[:])
	fmt.Appendf(&f, "%d:%d: %v", p.line, p.col, err)
	p.err.n = f.Len()
	return &p.err
}

func (p *Parser) Parse(src string) error {
	p.src, p.line, p.col, p.depth = src, 1, 0, 0
	if len(src) == 0 {
		return p.fail(ErrEmpty)
	}
	for i := 0; i < len(src); i++ {
		p.col++
		switch src[i] {
		case '\n':
			p.line++
			p.col = 0
		case '(':
			p.depth++
			if p.depth > 2 {
				return p.fail(ErrTooDeep)
			}
		case ')':
			p.depth--
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// Three layers: syscall (Errno), os (PathError), config (Errorf), and the
// program on top (Errorf again).

func loadConfig(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	f.Close()
	alloc.Free(f)
	return nil
}

func startup(name string) error {
	if err := loadConfig(name); err != nil {
		return fmt.Errorf("startup: %w", err)
	}
	return nil
}

// ----------------------------------------------------------------------------
// An allocator that counts what is outstanding, to check errors.Free.

type counting struct {
	base         alloc.Allocator
	live, allocs int
}

func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.live += int(size)
	c.allocs++
	return c.base.Alloc(size, align)
}

func (c *counting) Free(p unsafe.Pointer, size, align uintptr) {
	c.live -= int(size)
	c.base.Free(p, size, align)
}

// ----------------------------------------------------------------------------
// Custom matching.

type timeoutError struct{ op string }

func (e *timeoutError) Error() string        { return e.op }
func (e *timeoutError) Is(target error) bool { return target == ErrTimeout }

var ErrTimeout = errors.New("timeout")

var slowRead = &timeoutError{op: "read timed out"}

// listError is not comparable; errors.Is must not compare it with ==.
type listError struct{ items []int }

func (e listError) Error() string { return "list error" }

type shortReader struct{ n int }

func (r *shortReader) Read(p []byte) (int, error) {
	if r.n == 0 {
		return 0, io.EOF
	}
	k := min(r.n, len(p))
	for i := 0; i < k; i++ {
		p[i] = 'x'
	}
	r.n -= k
	return k, nil
}

func main() {
	// Sentinels.
	fmt.Println("sentinel:", ErrEmpty, ErrEmpty == ErrEmpty, ErrEmpty == ErrTooDeep)
	fmt.Println("is:", errors.Is(ErrEmpty, ErrEmpty), errors.Is(ErrEmpty, ErrTooDeep), errors.Is(nil, ErrEmpty), errors.Is(nil, nil))

	// Errno values.
	var e error = syscall.ENOENT
	fmt.Printf("errno: %v | %s | %d | %q\n", e, e, syscall.EACCES, syscall.EAGAIN)
	fmt.Println("errno name:", syscall.ENOENT.Name(), syscall.ECONNREFUSED.Name(), syscall.Errno(999).Name() == "")
	fmt.Println("errno unknown:", syscall.Errno(999))
	fmt.Println("errno is:", errors.Is(e, os.ErrNotExist), errors.Is(syscall.EACCES, os.ErrPermission),
		errors.Is(syscall.EPERM, os.ErrPermission), errors.Is(syscall.EEXIST, os.ErrExist), errors.Is(e, os.ErrExist),
		errors.Is(syscall.ENOSYS, errors.ErrUnsupported))
	fmt.Println("errno temp:", syscall.EINTR.Temporary(), syscall.EAGAIN.Timeout(), syscall.ENOENT.Temporary())

	// os.Open fails with a PathError that keeps the path string:
	// strings.FromUTF8Lossy copied the bytes, so changing them later does not
	// change the error.
	var name [32]byte
	n := copy(name[:], "/nonexistent/pluto.conf")
	_, err := os.Open(strings.FromUTF8Lossy(name[:n]))
	copy(name[:], "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX")
	fmt.Println("open:", err)
	var pe *os.PathError
	if errors.As(err, &pe) {
		fmt.Printf("path error: op=%s path=%s err=%v errno=%t\n", pe.Op, pe.Path, pe.Err, pe.Err == syscall.ENOENT)
	}
	fmt.Println("open is:", errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOENT), errors.Is(err, os.ErrExist))
	fmt.Println("unwrap:", errors.Unwrap(err) == syscall.ENOENT, errors.Unwrap(errors.Unwrap(err)) == nil)
	errors.Free(err)

	// A parser error with a line and column, in the parser's storage.
	var p Parser
	for _, src := range []string{"", "(a)\n(b)", "(a\n (b\n  (c)))"} {
		err := p.Parse(src)
		fmt.Printf("parse %q: %v\n", src, err)
		var perr *ParseError
		if errors.As(err, &perr) {
			fmt.Println("  at", perr.Line, perr.Col, errors.Is(err, ErrTooDeep), errors.Is(err, ErrEmpty))
		}
	}

	// Wrapping across three layers, inspected at the top.
	c := counting{base: alloc.Default()}
	restore := alloc.Use(&c)
	err = startup("/nonexistent/app.conf")
	restore.Restore()
	fmt.Println("startup:", err)
	fmt.Println("startup is:", errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENOENT), errors.Is(err, ErrEmpty))
	var pe2 *os.PathError
	// Printed from a function literal: the interface boxes of its Println
	// hold copies of the path string until it returns (language spec
	// section 4.4), not until main does, so the free below frees it.
	func() { fmt.Println("startup as:", errors.As(err, &pe2), pe2.Op, pe2.Path) }()
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		fmt.Println("startup errno:", errno.Name(), int(errno))
	}
	depth := 0
	for x := err; x != nil; x = errors.Unwrap(x) {
		depth++
	}
	fmt.Println("chain length:", depth, "allocations:", c.allocs, "live:", c.live > 0)
	errors.Free(err)
	fmt.Println("after free live:", c.live)

	// Errorf with several %w, and Join.
	restore = alloc.Use(&c)
	multi := fmt.Errorf("both: %w and %w", ErrEmpty, syscall.EIO)
	joined := errors.Join(ErrTooDeep, nil, multi, slowRead)
	restore.Restore()
	fmt.Printf("multi: %v\n", multi)
	fmt.Println("multi is:", errors.Is(multi, ErrEmpty), errors.Is(multi, syscall.EIO), errors.Is(multi, ErrTooDeep), errors.Unwrap(multi) == nil)
	fmt.Printf("joined:\n%v\n", joined)
	fmt.Println("joined is:", errors.Is(joined, syscall.EIO), errors.Is(joined, ErrTimeout), errors.Is(joined, io.EOF))
	var te *timeoutError
	fmt.Println("joined as:", errors.As(joined, &te), te.op)
	fmt.Println("join nil:", errors.Join(nil, nil) == nil)
	errors.Free(joined)
	fmt.Println("after free live:", c.live, "allocations:", c.allocs)

	// Errorf without %w, and %w outside Errorf.
	plain := fmt.Errorf("code %d", 42)
	fmt.Println("plain:", plain, errors.Unwrap(plain) == nil)
	errors.Free(plain)
	fmt.Printf("printf: %w|%x|%X\n", ErrEmpty, ErrEmpty, syscall.EIO)

	// An ErrorBuf owned by the caller.
	var eb fmt.ErrorBuf
	err = eb.Errorf("device %s: %w", "sda", syscall.EIO)
	fmt.Println("errorbuf:", err, errors.Is(err, syscall.EIO), errors.Unwrap(err) == syscall.EIO)
	err = eb.Errorf("%s", "a message that is longer than the two hundred bytes the buffer holds, so it is cut short at the end of the buffer instead of overflowing it; that is the documented behaviour of ErrorBuf, which never allocates and never panics.")
	fmt.Println("errorbuf long:", len(err.Error()))

	// Custom Is, and an uncomparable target.
	fmt.Println("custom is:", errors.Is(slowRead, ErrTimeout), errors.Is(ErrTimeout, slowRead))
	var le error = listError{items: []int{1}}
	fmt.Println("uncomparable:", errors.Is(slowRead, le), errors.Is(le, le))

	// io and strconv sentinels.
	var buf [8]byte
	r := &shortReader{n: 3}
	k, err := io.ReadFull(r, buf[:])
	fmt.Println("readfull:", k, err, err == io.ErrUnexpectedEOF)
	k, err = io.ReadFull(r, buf[:])
	fmt.Println("readfull:", k, err, err == io.EOF)
	_, err = strconv.Atoi("12x")
	fmt.Println("atoi:", err, errors.Is(err, strconv.ErrSyntax))
	_, err = strconv.ParseInt("99999999999999999999", 10, 64)
	fmt.Println("atoi:", err, errors.Is(err, strconv.ErrRange))
}
