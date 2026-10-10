package os

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/io"
	"pluto/rt"
	"pluto/syscall"
	"pluto/time"
	"pluto/vec"
	"unsafe"
)

// The file system: file information, whole-file reads and writes,
// directories, renaming and removal, the working directory and temporary
// directories.
//
// Functions that fail on a path report a *PathError, allocated from their
// allocator argument (by default the default allocator) as Open's are.
// Functions that produce text take a buffer from the caller and return a
// view of it; functions that produce a list of values return an owner
// (vec.Vec or DirEntries) that records its allocator.

// maxPath is the longest path, including its NUL, the system calls take.
const maxPath = 4096

// cpath copies name into buf with a terminating NUL, for a system call. It
// fails for names that are too long or contain a NUL.
func cpath(name string, buf *[maxPath]byte) ([]byte, error) {
	if len(name) >= len(buf) {
		return nil, syscall.ENAMETOOLONG
	}
	for i := 0; i < len(name); i++ {
		if name[i] == 0 {
			return nil, syscall.EINVAL
		}
	}
	copy(buf[:], name)
	buf[len(name)] = 0
	return buf[:len(name)+1], nil
}

// A FileMode is a file's type and permission bits, as in Go.
type FileMode uint32

// The type and mode bits; ModePerm holds the Unix permission bits.
const (
	ModeDir        FileMode = 1 << 31 // d: a directory
	ModeAppend     FileMode = 1 << 30 // a: append-only
	ModeExclusive  FileMode = 1 << 29 // l: exclusive use
	ModeTemporary  FileMode = 1 << 28 // T: temporary file
	ModeSymlink    FileMode = 1 << 27 // L: symbolic link
	ModeDevice     FileMode = 1 << 26 // D: device file
	ModeNamedPipe  FileMode = 1 << 25 // p: named pipe (FIFO)
	ModeSocket     FileMode = 1 << 24 // S: Unix domain socket
	ModeSetuid     FileMode = 1 << 23 // u: setuid
	ModeSetgid     FileMode = 1 << 22 // g: setgid
	ModeCharDevice FileMode = 1 << 21 // c: character device, with ModeDevice
	ModeSticky     FileMode = 1 << 20 // t: sticky
	ModeIrregular  FileMode = 1 << 19 // ?: of no other known type

	ModeType          = ModeDir | ModeSymlink | ModeNamedPipe | ModeSocket | ModeDevice | ModeCharDevice | ModeIrregular
	ModePerm FileMode = 0o777
)

// IsDir reports whether m describes a directory.
func (m FileMode) IsDir() bool { return m&ModeDir != 0 }

// IsRegular reports whether m describes a regular file.
func (m FileMode) IsRegular() bool { return m&ModeType == 0 }

// Perm returns the Unix permission bits of m.
func (m FileMode) Perm() FileMode { return m & ModePerm }

// Type returns the type bits of m.
func (m FileMode) Type() FileMode { return m & ModeType }

// AppendString appends m as Go's FileMode.String formats it, such as
// "drwxr-xr-x" or "-rw-r--r--", and returns the extended buffer.
func (m FileMode) AppendString(dst []byte) []byte {
	const str = "dalTLDpSugct?"
	typed := false
	for i, c := range str {
		if m&(1<<uint(32-1-i)) != 0 {
			dst = append(dst, byte(c))
			typed = true
		}
	}
	if !typed {
		dst = append(dst, '-')
	}
	const rwx = "rwxrwxrwx"
	for i, c := range rwx {
		if m&(1<<uint(9-1-i)) != 0 {
			dst = append(dst, byte(c))
		} else {
			dst = append(dst, '-')
		}
	}
	return dst
}

// modeFromStat converts the st_mode of a stat.
func modeFromStat(m uint32) FileMode {
	mode := FileMode(m & 0o777)
	switch m & syscall.S_IFMT {
	case syscall.S_IFBLK:
		mode |= ModeDevice
	case syscall.S_IFCHR:
		mode |= ModeDevice | ModeCharDevice
	case syscall.S_IFDIR:
		mode |= ModeDir
	case syscall.S_IFIFO:
		mode |= ModeNamedPipe
	case syscall.S_IFLNK:
		mode |= ModeSymlink
	case syscall.S_IFSOCK:
		mode |= ModeSocket
	}
	if m&syscall.S_ISGID != 0 {
		mode |= ModeSetgid
	}
	if m&syscall.S_ISUID != 0 {
		mode |= ModeSetuid
	}
	if m&syscall.S_ISVTX != 0 {
		mode |= ModeSticky
	}
	return mode
}

// A FileInfo describes a file, as Stat reports it. Unlike Go's, it is a
// struct, not an interface; the methods have Go's names.
type FileInfo struct {
	name    string
	size    int64
	mode    FileMode
	modTime time.Time
	ino     uint64
	nlink   uint64
}

// Name returns the file's base name: a view of the path given to Stat, or
// of the File's name.
func (fi *FileInfo) Name() string { return fi.name }

// Size returns the length in bytes of a regular file.
func (fi *FileInfo) Size() int64 { return fi.size }

// Mode returns the file's mode bits.
func (fi *FileInfo) Mode() FileMode { return fi.mode }

// ModTime returns the modification time.
func (fi *FileInfo) ModTime() time.Time { return fi.modTime }

// IsDir reports whether the file is a directory.
func (fi *FileInfo) IsDir() bool { return fi.mode.IsDir() }

// Ino returns the file's inode number.
func (fi *FileInfo) Ino() uint64 { return fi.ino }

// Nlink returns the number of hard links to the file.
func (fi *FileInfo) Nlink() uint64 { return fi.nlink }

func fileInfo(name string, st *syscall.Stat_t) FileInfo {
	return FileInfo{
		name:    basename(name),
		size:    st.Size,
		mode:    modeFromStat(st.Mode),
		modTime: time.Unix(st.Mtim.Sec, st.Mtim.Nsec),
		ino:     st.Ino,
		nlink:   st.Nlink,
	}
}

// basename returns the last element of a path, without trailing slashes.
func basename(name string) string {
	i := len(name) - 1
	for i > 0 && name[i] == '/' {
		name = name[:i]
		i--
	}
	for i--; i >= 0; i-- {
		if name[i] == '/' {
			name = name[i+1:]
			break
		}
	}
	return name
}

// Stat returns a FileInfo describing the named file, following symbolic
// links.
func Stat(name string, a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	return statAt(name, 0, "stat", a)
}

// Lstat is Stat without following a final symbolic link: it describes the
// link itself.
func Lstat(name string, a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	return statAt(name, syscall.AT_SYMLINK_NOFOLLOW, "lstat", a)
}

func statAt(name string, flags int, op string, a alloc.Allocator) (FileInfo, error) {
	var buf [maxPath]byte
	p, err := cpath(name, &buf)
	if err != nil {
		return FileInfo{}, NewPathError(op, name, err, a)
	}
	var st syscall.Stat_t
	if err := syscall.Fstatat(syscall.AT_FDCWD, p, &st, flags); err != nil {
		return FileInfo{}, NewPathError(op, name, err, a)
	}
	return fileInfo(name, &st), nil
}

// Stat describes the open file. It reports a bare syscall.Errno, as the
// File's I/O methods do.
func (f *File) Stat() (FileInfo, error) {
	var st syscall.Stat_t
	if err := syscall.Fstat(f.fd, &st); err != nil {
		return FileInfo{}, err
	}
	return fileInfo(f.name, &st), nil
}

// Seek sets the offset for the next Read or Write (whence: io.SeekStart,
// io.SeekCurrent or io.SeekEnd) and returns the new offset.
func (f *File) Seek(offset int64, whence int) (int64, error) {
	return syscall.Seek(f.fd, offset, whence)
}

// Sync flushes the file's contents to its device.
func (f *File) Sync() error { return syscall.Fsync(f.fd) }

// Truncate changes the size of the file.
func (f *File) Truncate(size int64) error { return syscall.Ftruncate(f.fd, size) }

// ReadFile reads the named file and returns its contents in a vec.Vec that
// allocates from a (by default the default allocator); the caller owns it
// and frees it with Free. A failure is a *PathError from a.
func ReadFile(name string, a alloc.Allocator = alloc.Default()) (vec.Vec[byte], error) {
	data := vec.New[byte](a)
	f, err := OpenFile(name, syscall.O_RDONLY, 0, a)
	if err != nil {
		return data, err
	}
	defer alloc.Free(f, a)
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.size > 0 && fi.size < 1<<40 {
		data.Reserve(int(fi.size) + 1)
	}
	if err := readAll(f, &data); err != nil {
		data.Free()
		return data, NewPathError("read", name, err, a)
	}
	return data, nil
}

// readAll appends everything r yields to data.
func readAll(r io.Reader, data *vec.Vec[byte]) error {
	var chunk [16 << 10]byte
	for {
		n, err := r.Read(chunk[:])
		data.Append(chunk[:n]...)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// WriteFile writes data to the named file, creating it with permissions
// perm if necessary and truncating it otherwise.
func WriteFile(name string, data []byte, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	f, err := OpenFile(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, uint32(perm.Perm()), a)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	cerr := f.Close()
	alloc.Free(f, a)
	if err == nil {
		err = cerr
	}
	if err != nil {
		return NewPathError("write", name, err, a)
	}
	return nil
}

// Mkdir creates a directory with permissions perm (before the umask).
func Mkdir(name string, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	var buf [maxPath]byte
	p, err := cpath(name, &buf)
	if err == nil {
		err = syscall.Mkdirat(syscall.AT_FDCWD, p, uint32(perm.Perm()))
	}
	if err != nil {
		return NewPathError("mkdir", name, err, a)
	}
	return nil
}

// MkdirAll creates a directory and any parents it needs, with permissions
// perm. It does nothing if path is already a directory.
func MkdirAll(path string, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	var buf [maxPath]byte
	p, err := cpath(path, &buf)
	if err != nil {
		return NewPathError("mkdir", path, err, a)
	}
	if err := mkdirAll(p, len(path), uint32(perm.Perm())); err != nil {
		return NewPathError("mkdir", path, err, a)
	}
	return nil
}

// mkdirAll creates p[:n], NUL-terminated in p, and its parents.
func mkdirAll(p []byte, n int, perm uint32) error {
	var st syscall.Stat_t
	saved := p[n]
	p[n] = 0
	defer func() { p[n] = saved }()
	if err := syscall.Fstatat(syscall.AT_FDCWD, p, &st, 0); err == nil {
		if st.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			return nil
		}
		return syscall.ENOTDIR
	}
	// Create the parent first: strip trailing slashes, then the last
	// element.
	i := n
	for i > 0 && p[i-1] == '/' {
		i--
	}
	for i > 0 && p[i-1] != '/' {
		i--
	}
	if i > 1 {
		if err := mkdirAll(p, i-1, perm); err != nil {
			return err
		}
	}
	err := syscall.Mkdirat(syscall.AT_FDCWD, p, perm)
	if err == syscall.EEXIST {
		// Raced with another creator, or a trailing "." or "..": accept
		// a directory.
		if syscall.Fstatat(syscall.AT_FDCWD, p, &st, 0) == nil && st.Mode&syscall.S_IFMT == syscall.S_IFDIR {
			return nil
		}
	}
	return err
}

// Remove removes the named file or empty directory.
func Remove(name string, a alloc.Allocator = alloc.Default()) error {
	var buf [maxPath]byte
	p, err := cpath(name, &buf)
	if err == nil {
		err = syscall.Unlinkat(syscall.AT_FDCWD, p, 0)
		if err == syscall.EISDIR || err == syscall.EPERM {
			if derr := syscall.Unlinkat(syscall.AT_FDCWD, p, syscall.AT_REMOVEDIR); derr == nil {
				err = nil
			} else if derr != syscall.ENOTDIR {
				err = derr
			}
		}
	}
	if err != nil {
		return NewPathError("remove", name, err, a)
	}
	return nil
}

// RemoveAll removes path and everything it contains. It returns nil if
// path does not exist.
func RemoveAll(path string, a alloc.Allocator = alloc.Default()) error {
	if path == "" {
		return nil
	}
	if path == "." || path == ".." || len(path) >= 2 && path[len(path)-2:] == "/." || len(path) >= 3 && path[len(path)-3:] == "/.." {
		return NewPathError("RemoveAll", path, syscall.EINVAL, a)
	}
	var buf [maxPath]byte
	p, err := cpath(path, &buf)
	if err == nil {
		err = removeAllAt(syscall.AT_FDCWD, p)
	}
	if err != nil {
		return NewPathError("unlinkat", path, err, a)
	}
	return nil
}

// removeAllAt removes name (NUL-terminated) relative to dirfd, and
// everything under it.
func removeAllAt(dirfd int, name []byte) error {
	err := syscall.Unlinkat(dirfd, name, 0)
	if err == nil || err == syscall.ENOENT {
		return nil
	}
	if err != syscall.EISDIR && err != syscall.EPERM && err != syscall.EACCES {
		return err
	}
	fd, oerr := syscall.Openat(dirfd, name, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if oerr == syscall.ENOENT {
		return nil
	}
	if oerr != nil {
		if oerr == syscall.ENOTDIR {
			return err // not a directory: the unlink error stands
		}
		return oerr
	}
	var dents [4096]byte
	var child [256]byte
	for {
		// Remove a batch, then read the directory again from the start,
		// since removing entries while reading it may skip some.
		removed := 0
		var firstErr error
		for {
			n, rerr := syscall.Getdents64(fd, dents[:])
			if rerr != nil {
				syscall.Close(fd)
				return rerr
			}
			if n == 0 {
				break
			}
			for off := 0; off < n; {
				ent, _, reclen := syscall.ParseDirent(dents[off:n])
				off += reclen
				if ent == "." || ent == ".." {
					continue
				}
				copy(child[:], ent)
				child[len(ent)] = 0
				if cerr := removeAllAt(fd, child[:len(ent)+1]); cerr != nil {
					if firstErr == nil {
						firstErr = cerr
					}
				} else {
					removed++
				}
			}
		}
		if firstErr != nil || removed == 0 {
			syscall.Close(fd)
			if firstErr != nil {
				return firstErr
			}
			break
		}
		if _, serr := syscall.Seek(fd, 0, syscall.SEEK_SET); serr != nil {
			syscall.Close(fd)
			return serr
		}
	}
	err = syscall.Unlinkat(dirfd, name, syscall.AT_REMOVEDIR)
	if err == syscall.ENOENT {
		return nil
	}
	return err
}

// Rename renames (moves) oldpath to newpath, replacing newpath if it is
// a file.
func Rename(oldpath, newpath string, a alloc.Allocator = alloc.Default()) error {
	var ob, nb [maxPath]byte
	op, err := cpath(oldpath, &ob)
	if err != nil {
		return NewPathError("rename", oldpath, err, a)
	}
	np, err := cpath(newpath, &nb)
	if err == nil {
		err = syscall.Renameat(syscall.AT_FDCWD, op, syscall.AT_FDCWD, np)
	}
	if err != nil {
		return NewPathError("rename", oldpath, err, a)
	}
	return nil
}

// Chmod changes the permission bits of the named file.
func Chmod(name string, mode FileMode, a alloc.Allocator = alloc.Default()) error {
	var buf [maxPath]byte
	p, err := cpath(name, &buf)
	if err == nil {
		err = syscall.Fchmodat(syscall.AT_FDCWD, p, uint32(mode&(ModePerm|ModeSetuid|ModeSetgid|ModeSticky)))
	}
	if err != nil {
		return NewPathError("chmod", name, err, a)
	}
	return nil
}

// Getwd stores the working directory in buf and returns it, a view of buf.
// It fails with ERANGE if buf is too short; maxPath bytes always suffice.
func Getwd(buf []byte) (string, error) {
	n, err := syscall.Getcwd(buf)
	if err != nil {
		return "", err
	}
	return unsafe.String(unsafe.SliceData(buf), n-1), nil
}

// Chdir changes the working directory.
func Chdir(dir string, a alloc.Allocator = alloc.Default()) error {
	var buf [maxPath]byte
	p, err := cpath(dir, &buf)
	if err == nil {
		err = syscall.Chdir(p)
	}
	if err != nil {
		return NewPathError("chdir", dir, err, a)
	}
	return nil
}

// TempDir returns the directory for temporary files: $TMPDIR, or "/tmp".
func TempDir() string {
	if d := Getenv("TMPDIR"); d != "" {
		return d
	}
	return "/tmp"
}

// MkdirTemp creates a new directory in dir (TempDir if dir is "") whose
// name is pattern with its last "*" replaced by a random string (or the
// random string appended if pattern has no "*"), and returns its path,
// built in buf. The directory has permissions 0o700; removing it is the
// caller's job.
func MkdirTemp(dir, pattern string, buf []byte, a alloc.Allocator = alloc.Default()) (string, error) {
	if dir == "" {
		dir = TempDir()
	}
	prefix, suffix := pattern, ""
	for i := len(pattern) - 1; i >= 0; i-- {
		if pattern[i] == '/' {
			return "", NewPathError("mkdirtemp", pattern, ErrPatternHasSeparator, a)
		}
		if pattern[i] == '*' {
			prefix, suffix = pattern[:i], pattern[i+1:]
			break
		}
	}
	for try := 0; try < 10000; try++ {
		var rnd [8]byte
		syscall.Getrandom(rnd[:], 0)
		r := uint64(0)
		for _, b := range rnd {
			r = r<<8 | uint64(b)
		}
		name := buf[:0]
		if cap(name) < len(dir)+1+len(prefix)+20+len(suffix) {
			return "", NewPathError("mkdirtemp", dir, syscall.ENAMETOOLONG, a)
		}
		name = append(name, dir...)
		if len(dir) > 0 && dir[len(dir)-1] != '/' {
			name = append(name, '/')
		}
		name = append(name, prefix...)
		for r > 0 {
			name = append(name, byte('0'+r%10))
			r /= 10
		}
		name = append(name, suffix...)
		s := unsafe.String(unsafe.SliceData(name), len(name))
		var pb [maxPath]byte
		p, err := cpath(s, &pb)
		if err == nil {
			err = syscall.Mkdirat(syscall.AT_FDCWD, p, 0o700)
		}
		if err == nil {
			return s, nil
		}
		if err != syscall.EEXIST {
			return "", NewPathError("mkdirtemp", s, err, a)
		}
	}
	return "", NewPathError("mkdirtemp", dir, syscall.EEXIST, a)
}

// ErrPatternHasSeparator is reported by MkdirTemp for a pattern with a "/".
var ErrPatternHasSeparator = errors.New("pattern contains path separator")

// Pipe returns a connected pair of Files: reads from r return bytes
// written to w. The Files are allocated from a, as Open's are.
func Pipe(a alloc.Allocator = alloc.Default()) (r *File, w *File, err error) {
	var p [2]int32
	if err := syscall.Pipe2(&p, syscall.O_CLOEXEC); err != nil {
		return nil, nil, NewPathError("pipe", "", err, a)
	}
	r = alloc.New[File](a)
	r.fd, r.name = int(p[0]), "|0"
	w = alloc.New[File](a)
	w.fd, w.name = int(p[1]), "|1"
	return r, w, nil
}

// Getpid returns the process ID.
func Getpid() int { return syscall.Getpid() }

// Getuid returns the user ID.
func Getuid() int { return syscall.Getuid() }

// LookupEnv returns the value of the environment variable key and whether
// it is set.
func LookupEnv(key string) (string, bool) {
	env := rt.Environ()
	for i := 0; ; i++ {
		p := *(**byte)(unsafe.Add(env, i*8))
		if p == nil {
			return "", false
		}
		kv := cstring(p)
		if len(kv) > len(key) && kv[len(key)] == '=' && kv[:len(key)] == key {
			return kv[len(key)+1:], true
		}
	}
}

// Environ returns the environment as "key=value" strings, views of the
// process's environment, in a vec.Vec allocated from a.
func Environ(a alloc.Allocator = alloc.Default()) vec.Vec[string] {
	env := rt.Environ()
	out := vec.New[string](a)
	for i := 0; ; i++ {
		p := *(**byte)(unsafe.Add(env, i*8))
		if p == nil {
			return out
		}
		out.Push(cstring(p))
	}
}

// Args returns the command-line arguments, starting with the program name,
// as views of the process's arguments in a vec.Vec allocated from a.
func Args(a alloc.Allocator = alloc.Default()) vec.Vec[string] {
	n := NArgs()
	out := vec.WithCapacity[string](n, a)
	for i := 0; i < n; i++ {
		out.Push(Arg(i))
	}
	return out
}
