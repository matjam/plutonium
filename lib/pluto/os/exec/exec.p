// Package exec runs external commands, as Go's os/exec does, on Linux.
//
// The differences from Go:
//
//   - Command returns a Cmd, not a *Cmd. A Cmd owns the array behind its
//     Args, so it is an owner (language spec section 7.18): drop it with Drop
//     when done, and do not copy it once started.
//   - Without goroutines, the copying between the child's standard streams
//     and Stdin, Stdout and Stderr happens inside Wait (and Run, Output,
//     CombinedOutput), which polls the pipes until the child closes them.
//     A stream that is an *os.File is passed to the child directly.
//   - An unsuccessful exit is reported as an ExitError, a value that fits in
//     the interface's data word: errors.As(err, &e) with a `var e
//     exec.ExitError`, then e.ExitCode().
//   - Output and CombinedOutput return a vec.Vec[byte] the caller owns.
//   - LookPath returns a string as Go's does; AppendLookPath appends the
//     exact bytes of the path to a vec.Vec[byte] or a vec.Fixed[byte].
package exec

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/rt"
	"pluto/strings"
	"pluto/syscall"
	"pluto/vec"
	"unsafe"
)

// ErrNotFound is the error (wrapped in a *os.PathError) when LookPath, or
// starting a Cmd, finds no executable file of the given name in $PATH.
var ErrNotFound = errors.New("executable file not found in $PATH")

// Errors from misusing a Cmd.
var (
	ErrAlreadyStarted = errors.New("exec: already started")
	ErrNotStarted     = errors.New("exec: not started")
	ErrStdoutSet      = errors.New("exec: Stdout already set")
	ErrStderrSet      = errors.New("exec: Stderr already set")
)

// A Cmd is an external command being prepared or run. It owns the memory
// of its arguments and output, so it is an owner (language spec section
// 7.18), released by Drop.
type Cmd owner struct {
	// Path is the command to run: a path, or a name looked up in $PATH
	// when the Cmd starts if it contains no slash.
	Path string

	// Args holds the arguments, including the command name as Args[0].
	Args []string

	// Env holds the environment as "key=value" strings; nil means the
	// current process's environment.
	Env []string

	// Dir is the working directory of the command; "" means the current
	// one.
	Dir string

	// Stdin, Stdout and Stderr are the command's standard streams. nil
	// connects the stream to /dev/null; an *os.File is passed to the child
	// as it is; anything else is connected through a pipe that Wait
	// copies.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Process is the running process, after Start.
	Process os.Process

	// ProcessState describes the exited process, after Wait.
	ProcessState os.ProcessState

	argv     vec.Vec[string] // owns Args when the Cmd came from Command
	started  bool
	finished bool
	in       int    // parent's end of the stdin pipe, or -1
	out, err int    // parent's ends of the stdout and stderr pipes, or -1
	shared   bool   // stdout and stderr share one pipe (out)
	toClose  [3]int // the child's ends, closed after the child starts
}

// Command returns a Cmd that runs name with the given arguments. Its Args
// are name followed by arg, in an array the Cmd owns; Drop frees it.
func Command(name string, arg ...string) Cmd {
	c := Cmd{Path: name}
	c.argv = vec.WithCapacity[string](len(arg) + 1)
	c.argv.Push(name)
	c.argv.Append(arg...)
	c.Args = c.argv.Slice()
	return c
}

// Drop frees what the Cmd owns. A started Cmd must have been waited for.
func (c *Cmd) Drop() {
	c.argv.Free()
	c.Args = nil
}

// A sink receives one of the child's output streams: w, or the Vec v, or
// nothing.
type sink struct {
	w io.Writer
	v *vec.Vec[byte]
}

func (s sink) none() bool { return s.w == nil && s.v == nil }

// file returns the *os.File s writes to, if it is one.
func (s sink) file() *os.File {
	if f, ok := s.w.(*os.File); ok && f != nil {
		return f
	}
	return nil
}

func (s sink) write(p []byte) error {
	if s.v != nil {
		s.v.Append(p...)
		return nil
	}
	_, err := s.w.Write(p)
	return err
}

// same reports whether two sinks are the same destination.
func same(x, y sink) bool {
	if x.v != nil || y.v != nil {
		return x.v == y.v
	}
	if x.w == nil || y.w == nil || !rt.Comparable(x.w) {
		return false
	}
	return x.w == y.w
}

// Start starts the command without waiting for it.
func (c *Cmd) Start() error { return c.start(sink{w: c.Stdout}, sink{w: c.Stderr}) }

// Wait waits for a started command to exit, copying its piped streams
// meanwhile, and releases what Start set up. It returns nil if the command
// exited with status 0, an ExitError if it exited otherwise or was killed,
// or the error that stopped the copying.
func (c *Cmd) Wait() error { return c.wait(sink{w: c.Stdout}, sink{w: c.Stderr}) }

// Run starts the command and waits for it.
func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// Output runs the command and returns its standard output, in a vec.Vec
// that allocates from a (by default the default allocator).
func (c *Cmd) Output(a alloc.Allocator = alloc.Default()) (vec.Vec[byte], error) {
	out := vec.New[byte](a)
	if c.Stdout != nil {
		return out, ErrStdoutSet
	}
	o := sink{v: &out}
	e := sink{w: c.Stderr}
	if err := c.start(o, e); err != nil {
		return out, err
	}
	err := c.wait(o, e)
	return out, err
}

// CombinedOutput runs the command and returns its standard output and
// standard error together, as Output does.
func (c *Cmd) CombinedOutput(a alloc.Allocator = alloc.Default()) (vec.Vec[byte], error) {
	out := vec.New[byte](a)
	if c.Stdout != nil {
		return out, ErrStdoutSet
	}
	if c.Stderr != nil {
		return out, ErrStderrSet
	}
	o := sink{v: &out}
	if err := c.start(o, o); err != nil {
		return out, err
	}
	err := c.wait(o, o)
	return out, err
}

// childFd returns the descriptor the child gets for an output stream, and
// the parent's end of its pipe or -1.
func outputFd(s sink, devnull int) (child, parent int, err error) {
	if s.none() {
		return devnull, -1, nil
	}
	if f := s.file(); f != nil {
		return f.Fd(), -1, nil
	}
	var p [2]int32
	if err := syscall.Pipe2(&p, syscall.O_CLOEXEC); err != nil {
		return -1, -1, err
	}
	return int(p[1]), int(p[0]), nil
}

func (c *Cmd) start(out, errOut sink) error {
	if c.started {
		return ErrAlreadyStarted
	}
	c.started = true
	c.in, c.out, c.err = -1, -1, -1
	c.toClose = [3]int{-1, -1, -1}
	a := alloc.Default()

	// The program's path, NUL-terminated.
	var pathBuf [4096]byte
	pf := vec.NewFixed(pathBuf[:len(pathBuf)-1])
	err := AppendLookPath(&pf, c.Path)
	if err != nil {
		return err
	}
	pathb := pf.Slice()
	pathBuf[len(pathb)] = 0
	cpath := pathBuf[:len(pathb)+1]
	// pathb is a path found in $PATH, any bytes but NUL; an error holds it
	// as a string with U+FFFD for each invalid byte (os.NewPathErrorBytes).
	var dirBuf [4096]byte
	var cdir []byte
	if c.Dir != "" {
		if len(c.Dir) >= len(dirBuf) {
			return os.NewPathError("chdir", c.Dir, syscall.ENAMETOOLONG)
		}
		copy(dirBuf[:], c.Dir)
		cdir = dirBuf[:len(c.Dir)+1]
	}

	// argv and envp as C arrays, in one block.
	args := c.Args
	if len(args) == 0 {
		args = unsafe.Slice(&c.Path, 1)
	}
	// The child's environment is the caller's strings, or ours exactly:
	// the bytes of a variable that is not valid UTF-8 must reach the child
	// unchanged, which os.Environ's strings would not do.
	var envView vec.Vec[[]byte]
	env := c.Env
	var envBytes [][]byte
	if env == nil {
		envView = os.EnvironBytes(a)
		envBytes = envView.Slice()
	}
	block, size := cArrays(args, env, envBytes, a)
	envView.Free()
	argvp := block
	envpp := unsafe.Add(block, (len(args)+1)*8)

	// The child's standard streams.
	devnull := -1
	if c.Stdin == nil || out.none() || errOut.none() {
		devnull, err = syscall.Openat(syscall.AT_FDCWD, devNullPath[:], syscall.O_RDWR|syscall.O_CLOEXEC, 0)
		if err != nil {
			a.Free(block, size, 8)
			return os.NewPathError("open", "/dev/null", err)
		}
	}
	var fds [3]int
	switch in := c.Stdin.(type) {
	case nil:
		fds[0] = devnull
	case *os.File:
		fds[0] = in.Fd()
	default:
		var p [2]int32
		if err = syscall.Pipe2(&p, syscall.O_CLOEXEC); err == nil {
			fds[0], c.in = int(p[0]), int(p[1])
			c.toClose[0] = fds[0]
		}
	}
	if err == nil {
		fds[1], c.out, err = outputFd(out, devnull)
		if c.out >= 0 {
			c.toClose[1] = fds[1]
		}
	}
	if err == nil {
		if same(out, errOut) && c.out >= 0 {
			fds[2] = fds[1]
			c.shared = true
		} else {
			fds[2], c.err, err = outputFd(errOut, devnull)
			if c.err >= 0 {
				c.toClose[2] = fds[2]
			}
		}
	}
	var ep [2]int32 // reports a failed exec from the child
	if err == nil {
		err = syscall.Pipe2(&ep, syscall.O_CLOEXEC)
	}
	if err != nil {
		a.Free(block, size, 8)
		c.closeAll(devnull)
		return os.NewPathErrorBytes("fork/exec", pathb, err)
	}

	pid, err := syscall.Fork()
	if err == nil && pid == 0 {
		child(cpath, argvp, envpp, cdir, fds, int(ep[1]))
	}
	a.Free(block, size, 8)
	syscall.Close(int(ep[1]))
	if err != nil {
		syscall.Close(int(ep[0]))
		c.closeAll(devnull)
		return os.NewPathErrorBytes("fork/exec", pathb, err)
	}
	// Did the exec work? The pipe closes on exec; otherwise the child
	// writes its errno.
	var code [4]byte
	n, rerr := readFull(int(ep[0]), code[:])
	syscall.Close(int(ep[0]))
	c.closeChildEnds(devnull)
	if n == 4 || rerr != nil {
		var status int32
		for {
			rt.EnterSyscall()
			_, werr := syscall.Wait4(pid, &status, 0)
			rt.ExitSyscall()
			if werr != syscall.EINTR {
				break
			}
		}
		c.closeParentEnds()
		c.finished = true
		if rerr == nil {
			rerr = syscall.Errno(uint32(code[0]) | uint32(code[1])<<8 | uint32(code[2])<<16 | uint32(code[3])<<24)
		}
		return os.NewPathErrorBytes("fork/exec", pathb, rerr)
	}
	c.Process = os.Process{Pid: pid}
	return nil
}

// devNullPath is "/dev/null", NUL-terminated.
var devNullPath = [...]byte{'/', 'd', 'e', 'v', '/', 'n', 'u', 'l', 'l', 0}

// child runs in the new process: it sets up the standard streams and the
// working directory and executes the program. It must not allocate.
func child(path []byte, argv, envp unsafe.Pointer, dir []byte, fds [3]int, errfd int) {
	// Move descriptors that sit on 0-2 out of the way first, so that
	// installing one stream cannot close another's source.
	for i := 0; i < 3; i++ {
		if fds[i] < 3 && fds[i] != i {
			nfd, err := syscall.Fcntl(fds[i], fcntlDupfdCloexec, 3)
			if err != nil {
				childFail(errfd, err)
			}
			fds[i] = nfd
		}
	}
	for i := 0; i < 3; i++ {
		var err error
		if fds[i] == i {
			_, err = syscall.Fcntl(i, syscall.F_SETFD, 0)
		} else {
			err = syscall.Dup3(fds[i], i, 0)
		}
		if err != nil {
			childFail(errfd, err)
		}
	}
	if dir != nil {
		if err := syscall.Chdir(dir); err != nil {
			childFail(errfd, err)
		}
	}
	err := syscall.Execve(path, argv, envp)
	childFail(errfd, err)
}

const fcntlDupfdCloexec = 1030 // F_DUPFD_CLOEXEC

// childFail reports err to the parent through fd and exits.
func childFail(fd int, err error) {
	code := uint32(syscall.EINVAL)
	if e, ok := err.(syscall.Errno); ok {
		code = uint32(e)
	}
	b := [4]byte{byte(code), byte(code >> 8), byte(code >> 16), byte(code >> 24)}
	syscall.Write(fd, b[:])
	syscall.Exit(127)
}

// cArrays lays out argv and envp for execve in one block from a: the two
// nil-terminated pointer arrays, then the NUL-terminated strings. The
// environment is env, or envBytes (exact bytes) if env is nil. It returns
// the block and its size.
func cArrays(args, env []string, envBytes [][]byte, a alloc.Allocator) (unsafe.Pointer, uintptr) {
	ptrs := (len(args) + 1 + len(env) + len(envBytes) + 1) * 8
	size := ptrs
	for _, s := range args {
		size += len(s) + 1
	}
	for _, s := range env {
		size += len(s) + 1
	}
	for _, b := range envBytes {
		size += len(b) + 1
	}
	block := alloc.Raw(uintptr(size), 8, a)
	slot := 0
	text := ptrs
	put := func(s string) {
		p := unsafe.Add(block, text)
		copy(unsafe.Slice((*byte)(p), len(s)), s)
		*(*byte)(unsafe.Add(p, len(s))) = 0
		*(*uintptr)(unsafe.Add(block, slot*8)) = uintptr(p) // for the kernel: no tag
		slot++
		text += len(s) + 1
	}
	putBytes := func(b []byte) {
		p := unsafe.Add(block, text)
		copy(unsafe.Slice((*byte)(p), len(b)), b)
		*(*byte)(unsafe.Add(p, len(b))) = 0
		*(*uintptr)(unsafe.Add(block, slot*8)) = uintptr(p) // for the kernel: no tag
		slot++
		text += len(b) + 1
	}
	for _, s := range args {
		put(s)
	}
	slot++ // nil
	for _, s := range env {
		put(s)
	}
	for _, b := range envBytes {
		putBytes(b)
	}
	return block, uintptr(size)
}

// readFull reads into b until it is full or the descriptor reaches EOF,
// and returns how much it read. The reads are announced to the scheduler
// (rt.EnterSyscall), so other tasks keep running while it waits.
func readFull(fd int, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		rt.EnterSyscall()
		m, err := syscall.Read(fd, b[n:])
		rt.ExitSyscall()
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return n, err
		}
		if m == 0 {
			break
		}
		n += m
	}
	return n, nil
}

func (c *Cmd) closeChildEnds(devnull int) {
	for i, fd := range c.toClose {
		if fd >= 0 {
			syscall.Close(fd)
			c.toClose[i] = -1
		}
	}
	if devnull >= 0 {
		syscall.Close(devnull)
	}
}

func (c *Cmd) closeParentEnds() {
	for _, p := range [...]*int{&c.in, &c.out, &c.err} {
		if *p >= 0 {
			syscall.Close(*p)
			*p = -1
		}
	}
}

func (c *Cmd) closeAll(devnull int) {
	c.closeChildEnds(devnull)
	c.closeParentEnds()
}

func (c *Cmd) wait(out, errOut sink) error {
	if !c.started {
		return ErrNotStarted
	}
	if c.finished {
		return errors.New("exec: Wait was already called")
	}
	c.finished = true
	copyErr := c.pump(out, errOut)
	c.closeParentEnds()
	state := c.Process.Wait() or return
	c.ProcessState = state
	if !state.Success() {
		return ExitError(uint64(uint32(state.Pid()))<<32 | uint64(state.Sys()))
	}
	return copyErr
}

// pump copies between the parent's pipe ends and the Cmd's streams until
// the child has closed its output pipes and the input is exhausted.
func (c *Cmd) pump(out, errOut sink) error {
	var firstErr error
	var buf [16 << 10]byte
	var inBuf [16 << 10]byte
	inData := inBuf[:0]
	inEOF := false
	for c.in >= 0 || c.out >= 0 || c.err >= 0 {
		var fds [3]syscall.PollFd
		n := 0
		if c.in >= 0 {
			if len(inData) == 0 && !inEOF {
				m, err := c.Stdin.Read(inBuf[:])
				inData = inBuf[:m]
				if err != nil {
					inEOF = true
					if err != io.EOF && firstErr == nil {
						firstErr = err
					}
				}
			}
			if len(inData) == 0 && inEOF {
				syscall.Close(c.in)
				c.in = -1
			} else {
				fds[n] = syscall.PollFd{Fd: int32(c.in), Events: syscall.POLLOUT}
				n++
			}
		}
		if c.out >= 0 {
			fds[n] = syscall.PollFd{Fd: int32(c.out), Events: syscall.POLLIN}
			n++
		}
		if c.err >= 0 {
			fds[n] = syscall.PollFd{Fd: int32(c.err), Events: syscall.POLLIN}
			n++
		}
		if n == 0 {
			break
		}
		rt.EnterSyscall()
		_, err := syscall.Poll(fds[:n], -1)
		rt.ExitSyscall()
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return err
		}
		for i := 0; i < n; i++ {
			fd := int(fds[i].Fd)
			if fds[i].Revents == 0 {
				continue
			}
			switch fd {
			case c.in:
				m, err := syscall.Write(fd, inData)
				if err != nil && err != syscall.EINTR && err != syscall.EAGAIN {
					// The child stopped reading (EPIPE): drop the rest.
					syscall.Close(c.in)
					c.in = -1
					continue
				}
				inData = inData[m:]
			case c.out, c.err:
				m, err := syscall.Read(fd, buf[:])
				if err == syscall.EINTR {
					continue
				}
				if m > 0 {
					// Each sink is written through its own name: a
					// sink chosen into one variable would join the
					// two, and a Vec's store into itself would read as
					// a store of one sink's buffer into the other.
					var werr error
					if fd == c.err {
						werr = errOut.write(buf[:m])
					} else {
						werr = out.write(buf[:m])
					}
					if werr != nil && firstErr == nil {
						firstErr = werr
					}
				}
				if m == 0 || err != nil {
					syscall.Close(fd)
					if fd == c.out {
						c.out = -1
					} else {
						c.err = -1
					}
				}
			}
		}
	}
	return firstErr
}

// An ExitError reports a command that exited with a status other than 0 or
// was killed by a signal. It is a word holding the process ID and the wait
// status, so returning it never allocates.
type ExitError uint64

// ExitCode returns the exit status, or -1 if a signal ended the process.
func (e ExitError) ExitCode() int { return syscall.WaitStatus(uint32(e)).ExitStatus() }

// Exited reports whether the process exited on its own.
func (e ExitError) Exited() bool { return syscall.WaitStatus(uint32(e)).Exited() }

// Pid returns the process ID.
func (e ExitError) Pid() int { return int(e >> 32) }

// Sys returns the raw wait status.
func (e ExitError) Sys() syscall.WaitStatus { return syscall.WaitStatus(uint32(e)) }

// exitTexts holds the messages of ExitError.Error, made the first time
// each is needed: "exit status N" for N in [0, 255] and "signal: N" for
// signals; static storage, so the messages can be returned as views.
var exitTexts [256 + 65][16]byte

// Error returns "exit status N", or "signal: N" for a process a signal
// ended (Go names the signal; this prints its number, except "signal:
// killed" and "signal: terminated").
func (e ExitError) Error() string {
	w := syscall.WaitStatus(uint32(e))
	if w.Signaled() {
		switch w.Signal() {
		case syscall.SIGKILL:
			return "signal: killed"
		case syscall.SIGTERM:
			return "signal: terminated"
		case syscall.SIGINT:
			return "signal: interrupt"
		}
		return text(256+w.Signal()&63, "signal: ", w.Signal())
	}
	return text(w.ExitStatus()&255, "exit status ", w.ExitStatus())
}

// text returns slot i of exitTexts, filled with prefix and n.
func text(i int, prefix string, n int) string {
	b := vec.NewFixed(exitTexts[i][:])
	vec.AppendString(&b, prefix)
	if n >= 100 {
		b.Push(byte('0' + n/100))
	}
	if n >= 10 {
		b.Push(byte('0' + n/10%10))
	}
	b.Push(byte('0' + n%10))
	// ASCII only (the prefixes and digits above), so valid UTF-8.
	return unsafe.String(&exitTexts[i][0], b.Len())
}

// LookPath finds the executable file to run for file: file itself if it
// contains a slash, otherwise the first executable regular file named file
// in a directory of $PATH. The result is file itself or a new string. A
// failure is a *os.PathError from the default allocator wrapping
// ErrNotFound or the reason file cannot be run. A path found in $PATH that
// is not valid UTF-8 is returned with U+FFFD for each invalid byte; use
// AppendLookPath for the exact bytes.
func LookPath(file string) (string, error) {
	for i := 0; i < len(file); i++ {
		if file[i] == '/' {
			if err := executable(strBytes(file)); err != nil {
				return "", os.NewPathError("exec", file, err)
			}
			return file, nil
		}
	}
	var buf [4096]byte
	b := vec.NewFixed(buf[:])
	if err := AppendLookPath(&b, file); err != nil {
		return "", err
	}
	// A directory of $PATH may not be valid UTF-8: strings.FromUTF8Lossy replaces each
	// invalid byte with U+FFFD. AppendLookPath returns the exact bytes.
	return strings.FromUTF8Lossy(b.Slice()), nil
}

// AppendLookPath is LookPath appending the path it finds to dst, a
// *vec.Vec[byte] (which grows) or a *vec.Fixed[byte]; it allocates only for
// an error or to grow a Vec. A path that does not fit in a Fixed is
// ENAMETOOLONG, and dst is left as it was. The path is the exact bytes:
// $PATH is searched as bytes, so a directory that is not valid UTF-8 works.
func AppendLookPath[D vec.Appender[byte]](dst D, file string) error {
	for i := 0; i < len(file); i++ {
		if file[i] == '/' {
			if err := executable(strBytes(file)); err != nil {
				return os.NewPathError("exec", file, err)
			}
			if !appendAll(dst, strBytes(file)) {
				return os.NewPathError("exec", file, syscall.ENAMETOOLONG)
			}
			return nil
		}
	}
	if file == "" {
		return os.NewPathError("exec", file, ErrNotFound)
	}
	var buf [4096]byte
	path := os.GetenvBytes(strBytes("PATH"))
	for len(path) > 0 {
		dir := path
		var rest []byte
		for i := 0; i < len(path); i++ {
			if path[i] == ':' {
				dir, rest = path[:i], path[i+1:]
				break
			}
		}
		if len(dir) == 0 {
			dir = strBytes(".")
		}
		if len(dir)+1+len(file) <= len(buf) {
			b := vec.NewFixed(buf[:])
			b.Append(dir...)
			b.Push('/')
			vec.AppendString(&b, file)
			if executable(b.Slice()) == nil {
				if !appendAll(dst, b.Slice()) {
					return os.NewPathErrorBytes("exec", b.Slice(), syscall.ENAMETOOLONG)
				}
				return nil
			}
		}
		if len(rest) == 0 {
			break
		}
		path = rest
	}
	return os.NewPathError("exec", file, ErrNotFound)
}

// appendAll appends b to dst whole, or reports false and leaves dst as it
// was if a bounded dst cannot hold all of it: a path must not be cut.
func appendAll[D vec.Appender[byte]](dst D, b []byte) bool {
	n := dst.Len()
	dst.Append(b...)
	if dst.Len()-n < len(b) {
		dst.Truncate(n)
		return false
	}
	return true
}

// strBytes views a string's bytes. The caller must not modify them.
func strBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// executable reports why path is not an executable regular file, or nil.
func executable(path []byte) error {
	var buf [4096]byte
	if len(path) >= len(buf) {
		return syscall.ENAMETOOLONG
	}
	copy(buf[:], path)
	p := buf[:len(path)+1]
	var st syscall.Stat_t
	if err := syscall.Fstatat(syscall.AT_FDCWD, p, &st, 0); err != nil {
		return err
	}
	if st.Mode&syscall.S_IFMT == syscall.S_IFDIR {
		return syscall.EISDIR
	}
	if st.Mode&0o111 == 0 {
		return syscall.EACCES
	}
	return syscall.Faccessat(syscall.AT_FDCWD, p, syscall.X_OK)
}
