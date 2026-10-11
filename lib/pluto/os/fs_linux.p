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
//
// Names and paths. A file name on Linux is any bytes but NUL and '/'. A
// string holds only valid UTF-8 (language spec section 18.8), so every name
// or path this package hands back as a string (DirEntry.Name, Readlink,
// Getwd, FileInfo.Name, PathError.Path, File.Name, Arg, Getenv) has each
// invalid byte replaced by U+FFFD, as strings.FromUTF8Lossy does, and the exact bytes
// are available next to it: DirEntry.RawName, AppendReadlink, AppendWd,
// ArgBytes, GetenvBytes. A lossy name cannot be opened, because it may name
// another file or none, so every core operation on a path has a Bytes form
// that takes the path as a []byte (OpenBytes, StatBytes, RemoveBytes and so
// on). Use them with RawName and the Append forms for files whose names
// may not be UTF-8.

// maxPath is the longest path, including its NUL, the system calls take.
const maxPath = 4096

// cpath copies name into buf with a terminating NUL, for a system call. It
// fails for names that are too long or contain a NUL.
func cpath(name string, buf *[maxPath]byte) ([]byte, error) {
	return cpathBytes(strBytes(name), buf)
}

// cpathBytes is cpath for a name that is bytes.
func cpathBytes(name []byte, buf *[maxPath]byte) ([]byte, error) {
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

// String returns m as Go's FileMode.String formats it, such as
// "drwxr-xr-x" or "-rw-r--r--", in a new string.
func (m FileMode) String() string {
	var buf [32]byte
	b := vec.NewFixed(buf[:])
	m.AppendString(&b)
	return rt.StringFromSlice(b.Slice(), alloc.Default())
}

// AppendString appends m as String formats it to dst, a *vec.Vec[byte]
// (which grows) or a *vec.Fixed[byte] (which keeps what fits).
func (m FileMode) AppendString[D vec.Appender[byte]](dst D) {
	const str = "dalTLDpSugct?"
	typed := false
	for i, c := range str {
		if m&(1<<uint(32-1-i)) != 0 {
			dst.Push(byte(c))
			typed = true
		}
	}
	if !typed {
		dst.Push('-')
	}
	const rwx = "rwxrwxrwx"
	for i, c := range rwx {
		if m&(1<<uint(9-1-i)) != 0 {
			dst.Push(byte(c))
		} else {
			dst.Push('-')
		}
	}
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
	raw     []byte // the exact name, a view; see FileInfoToDirEntryBytes
	size    int64
	mode    FileMode
	modTime time.Time
	ino     uint64
	nlink   uint64
}

// Name returns the file's base name: a view of the path given to Stat, or
// of the File's name. If the name is not valid UTF-8 (StatBytes, ReadDir)
// it is a copy with U+FFFD for each invalid byte; the caller has the exact
// bytes (the path given to StatBytes, DirEntry.RawName).
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
	base := basename(name)
	fi := fileInfoOf(st)
	fi.name = base
	fi.raw = strBytes(base)
	return fi
}

// fileInfoOf describes st, without a name.
func fileInfoOf(st *syscall.Stat_t) FileInfo {
	return FileInfo{
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

// basenameBytes is basename for a path that is bytes.
func basenameBytes(name []byte) []byte {
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
	return statAt(strBytes(name), name, 0, "stat", a)
}

// StatBytes is Stat for a name that is bytes (see OpenBytes). The FileInfo's
// Name is a copy of the base name with invalid bytes replaced by U+FFFD.
func StatBytes(name []byte, a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	return statAt(name, "", 0, "stat", a)
}

// Lstat is Stat without following a final symbolic link: it describes the
// link itself.
func Lstat(name string, a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	return statAt(strBytes(name), name, syscall.AT_SYMLINK_NOFOLLOW, "lstat", a)
}

// LstatBytes is Lstat for a name that is bytes (see OpenBytes).
func LstatBytes(name []byte, a alloc.Allocator = alloc.Default()) (FileInfo, error) {
	return statAt(name, "", syscall.AT_SYMLINK_NOFOLLOW, "lstat", a)
}

// statAt stats name, whose string form sname is "" if the caller has only
// bytes.
func statAt(name []byte, sname string, flags int, op string, a alloc.Allocator) (FileInfo, error) {
	var st syscall.Stat_t
	if err := fstatat(name, sname, &st, flags, op, a); err != nil {
		return FileInfo{}, err
	}
	if sname != "" || len(name) == 0 {
		return fileInfo(sname, &st), nil
	}
	fi := fileInfoOf(&st)
	fi.raw = basenameBytes(name)
	fi.name = lossyCopy(fi.raw, a)
	return fi, nil
}

// fstatat fills st for name, whose string form sname is "" if the caller
// has only bytes, or returns a *PathError.
func fstatat(name []byte, sname string, st *syscall.Stat_t, flags int, op string, a alloc.Allocator) error {
	var buf [maxPath]byte
	p, err := cpathBytes(name, &buf)
	if err != nil {
		return newPathError(op, name, sname, err, a)
	}
	if err := syscall.Fstatat(syscall.AT_FDCWD, p, st, flags); err != nil {
		return newPathError(op, name, sname, err, a)
	}
	return nil
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
	return readFile(strBytes(name), name, a)
}

// ReadFileBytes is ReadFile for a name that is bytes (see OpenBytes).
func ReadFileBytes(name []byte, a alloc.Allocator = alloc.Default()) (vec.Vec[byte], error) {
	return readFile(name, "", a)
}

func readFile(name []byte, sname string, a alloc.Allocator) (vec.Vec[byte], error) {
	data := vec.New[byte](a)
	f, err := openFile(name, sname, syscall.O_RDONLY, 0, a)
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
		return data, newPathError("read", name, sname, err, a)
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
	return writeFile(strBytes(name), name, data, perm, a)
}

// WriteFileBytes is WriteFile for a name that is bytes (see OpenBytes).
func WriteFileBytes(name []byte, data []byte, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	return writeFile(name, "", data, perm, a)
}

func writeFile(name []byte, sname string, data []byte, perm FileMode, a alloc.Allocator) error {
	f, err := openFile(name, sname, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_TRUNC, uint32(perm.Perm()), a)
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
		return newPathError("write", name, sname, err, a)
	}
	return nil
}

// Mkdir creates a directory with permissions perm (before the umask).
func Mkdir(name string, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	return mkdir(strBytes(name), name, perm, a)
}

// MkdirBytes is Mkdir for a name that is bytes (see OpenBytes).
func MkdirBytes(name []byte, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	return mkdir(name, "", perm, a)
}

func mkdir(name []byte, sname string, perm FileMode, a alloc.Allocator) error {
	var buf [maxPath]byte
	p, err := cpathBytes(name, &buf)
	if err == nil {
		err = syscall.Mkdirat(syscall.AT_FDCWD, p, uint32(perm.Perm()))
	}
	if err != nil {
		return newPathError("mkdir", name, sname, err, a)
	}
	return nil
}

// MkdirAll creates a directory and any parents it needs, with permissions
// perm. It does nothing if path is already a directory.
func MkdirAll(path string, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	return mkdirAllPath(strBytes(path), path, perm, a)
}

// MkdirAllBytes is MkdirAll for a path that is bytes (see OpenBytes).
func MkdirAllBytes(path []byte, perm FileMode, a alloc.Allocator = alloc.Default()) error {
	return mkdirAllPath(path, "", perm, a)
}

func mkdirAllPath(path []byte, spath string, perm FileMode, a alloc.Allocator) error {
	var buf [maxPath]byte
	p, err := cpathBytes(path, &buf)
	if err != nil {
		return newPathError("mkdir", path, spath, err, a)
	}
	if err := mkdirAll(p, len(path), uint32(perm.Perm())); err != nil {
		return newPathError("mkdir", path, spath, err, a)
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
	return remove(strBytes(name), name, a)
}

// RemoveBytes is Remove for a name that is bytes (see OpenBytes).
func RemoveBytes(name []byte, a alloc.Allocator = alloc.Default()) error {
	return remove(name, "", a)
}

func remove(name []byte, sname string, a alloc.Allocator) error {
	var buf [maxPath]byte
	p, err := cpathBytes(name, &buf)
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
		return newPathError("remove", name, sname, err, a)
	}
	return nil
}

// RemoveAll removes path and everything it contains. It returns nil if
// path does not exist.
func RemoveAll(path string, a alloc.Allocator = alloc.Default()) error {
	return removeAll(strBytes(path), path, a)
}

// RemoveAllBytes is RemoveAll for a path that is bytes (see OpenBytes).
func RemoveAllBytes(path []byte, a alloc.Allocator = alloc.Default()) error {
	return removeAll(path, "", a)
}

func removeAll(path []byte, spath string, a alloc.Allocator) error {
	if len(path) == 0 {
		return nil
	}
	if hasDotSuffix(path) {
		return newPathError("RemoveAll", path, spath, syscall.EINVAL, a)
	}
	var buf [maxPath]byte
	p, err := cpathBytes(path, &buf)
	if err == nil {
		err = removeAllAt(syscall.AT_FDCWD, p)
	}
	if err != nil {
		return newPathError("unlinkat", path, spath, err, a)
	}
	return nil
}

// hasDotSuffix reports whether path is "." or "..", or ends in "/." or
// "/..".
func hasDotSuffix(path []byte) bool {
	n := len(path)
	if n == 1 && path[0] == '.' || n == 2 && path[0] == '.' && path[1] == '.' {
		return true
	}
	if n >= 2 && path[n-2] == '/' && path[n-1] == '.' {
		return true
	}
	return n >= 3 && path[n-3] == '/' && path[n-2] == '.' && path[n-1] == '.'
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
				if isDots(ent) {
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
	return rename(strBytes(oldpath), oldpath, strBytes(newpath), a)
}

// RenameBytes is Rename for names that are bytes (see OpenBytes).
func RenameBytes(oldpath, newpath []byte, a alloc.Allocator = alloc.Default()) error {
	return rename(oldpath, "", newpath, a)
}

func rename(oldpath []byte, soldpath string, newpath []byte, a alloc.Allocator) error {
	var ob, nb [maxPath]byte
	op, err := cpathBytes(oldpath, &ob)
	if err != nil {
		return newPathError("rename", oldpath, soldpath, err, a)
	}
	np, err := cpathBytes(newpath, &nb)
	if err == nil {
		err = syscall.Renameat(syscall.AT_FDCWD, op, syscall.AT_FDCWD, np)
	}
	if err != nil {
		return newPathError("rename", oldpath, soldpath, err, a)
	}
	return nil
}

// Chmod changes the permission bits of the named file.
func Chmod(name string, mode FileMode, a alloc.Allocator = alloc.Default()) error {
	return chmod(strBytes(name), name, mode, a)
}

// ChmodBytes is Chmod for a name that is bytes (see OpenBytes).
func ChmodBytes(name []byte, mode FileMode, a alloc.Allocator = alloc.Default()) error {
	return chmod(name, "", mode, a)
}

func chmod(name []byte, sname string, mode FileMode, a alloc.Allocator) error {
	var buf [maxPath]byte
	p, err := cpathBytes(name, &buf)
	if err == nil {
		err = syscall.Fchmodat(syscall.AT_FDCWD, p, uint32(mode&(ModePerm|ModeSetuid|ModeSetgid|ModeSticky)))
	}
	if err != nil {
		return newPathError("chmod", name, sname, err, a)
	}
	return nil
}

// isDots reports whether name is "." or "..".
func isDots(name []byte) bool {
	return len(name) == 1 && name[0] == '.' || len(name) == 2 && name[0] == '.' && name[1] == '.'
}

// Readlink returns the target of the symbolic link name, a string with
// each invalid UTF-8 byte of the target replaced by U+FFFD (a link may
// point anywhere, and a string holds only valid UTF-8; see AppendReadlink
// for the exact bytes). A failure is a *PathError from a.
func Readlink(name string, a alloc.Allocator = alloc.Default()) (string, error) {
	var buf [maxPath]byte
	n, err := readlink(&buf, strBytes(name), name, a)
	if err != nil {
		return "", err
	}
	return lossyCopy(buf[:n], a), nil
}

// AppendReadlink appends the exact bytes of the target of the symbolic link
// name to dst, a *vec.Vec[byte] (which grows) or a *vec.Fixed[byte], and
// allocates only for an error or to grow a Vec. A target that does not fit
// in a Fixed is ENAMETOOLONG, and dst is left as it was; maxPath (4096)
// bytes always suffice.
func AppendReadlink[D vec.Appender[byte]](dst D, name string, a alloc.Allocator = alloc.Default()) error {
	return appendReadlink(dst, strBytes(name), name, a)
}

// ReadlinkBytes is AppendReadlink for a name that is bytes (see
// OpenBytes).
func ReadlinkBytes[D vec.Appender[byte]](dst D, name []byte, a alloc.Allocator = alloc.Default()) error {
	return appendReadlink(dst, name, "", a)
}

func appendReadlink[D vec.Appender[byte]](dst D, name []byte, sname string, a alloc.Allocator) error {
	var buf [maxPath]byte
	n, err := readlink(&buf, name, sname, a)
	if err != nil {
		return err
	}
	if !appendAll(dst, buf[:n]) {
		return newPathError("readlink", name, sname, syscall.ENAMETOOLONG, a)
	}
	return nil
}

// readlink reads the target of the symbolic link name into buf and returns
// its length.
func readlink(buf *[maxPath]byte, name []byte, sname string, a alloc.Allocator) (int, error) {
	var pbuf [maxPath]byte
	p, err := cpathBytes(name, &pbuf)
	if err != nil {
		return 0, newPathError("readlink", name, sname, err, a)
	}
	n, err := syscall.Readlinkat(syscall.AT_FDCWD, p, buf[:])
	if err != nil {
		return 0, newPathError("readlink", name, sname, err, a)
	}
	if n >= len(buf) {
		// The kernel cuts a longer target to fit: it may be truncated.
		return 0, newPathError("readlink", name, sname, syscall.ENAMETOOLONG, a)
	}
	return n, nil
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

// Getwd returns the working directory, a new string from the current
// allocator. The directory's name is any bytes but NUL, so each byte that
// is not valid UTF-8 is replaced by U+FFFD, as strings.FromUTF8Lossy does; AppendWd
// returns the exact bytes.
func Getwd() (string, error) {
	var buf [maxPath]byte
	n, err := getwd(&buf)
	if err != nil {
		return "", err
	}
	return rt.StringFromSlice(buf[:n], alloc.Default()), nil
}

// AppendWd appends the exact bytes of the working directory to dst, a
// *vec.Vec[byte] (which grows) or a *vec.Fixed[byte], and allocates only to
// grow a Vec. It fails with ERANGE, leaving dst as it was, if the name does
// not fit in a Fixed; maxPath bytes always suffice.
func AppendWd[D vec.Appender[byte]](dst D) error {
	var buf [maxPath]byte
	n, err := getwd(&buf)
	if err != nil {
		return err
	}
	if !appendAll(dst, buf[:n]) {
		return syscall.ERANGE
	}
	return nil
}

// getwd reads the working directory into buf and returns its length.
func getwd(buf *[maxPath]byte) (int, error) {
	n, err := syscall.Getcwd(buf[:])
	if err != nil {
		return 0, err
	}
	return n - 1, nil
}

// Chdir changes the working directory.
func Chdir(dir string, a alloc.Allocator = alloc.Default()) error {
	return chdir(strBytes(dir), dir, a)
}

// ChdirBytes is Chdir for a directory name that is bytes (see OpenBytes).
func ChdirBytes(dir []byte, a alloc.Allocator = alloc.Default()) error {
	return chdir(dir, "", a)
}

func chdir(dir []byte, sdir string, a alloc.Allocator) error {
	var buf [maxPath]byte
	p, err := cpathBytes(dir, &buf)
	if err == nil {
		err = syscall.Chdir(p)
	}
	if err != nil {
		return newPathError("chdir", dir, sdir, err, a)
	}
	return nil
}

// TempDir returns the directory for temporary files: $TMPDIR, or "/tmp".
// A $TMPDIR that is not valid UTF-8 comes back with U+FFFD for each invalid
// byte; TempDirBytes returns it exactly.
func TempDir() string {
	if d := Getenv("TMPDIR"); d != "" {
		return d
	}
	return "/tmp"
}

// TempDirBytes returns the directory for temporary files as the exact
// bytes of $TMPDIR, or "/tmp". It may be a view of the process's
// environment: do not modify it.
func TempDirBytes() []byte {
	if d := GetenvBytes(strBytes("TMPDIR")); len(d) > 0 {
		return d
	}
	return strBytes("/tmp")
}

// MkdirTemp creates a new directory in dir (TempDir if dir is "") whose
// name is pattern with its last "*" replaced by a random string (or the
// random string appended if pattern has no "*"), and returns its path, a
// new string. The directory has permissions 0o700; removing it is the
// caller's job. Errors, and the path, come from a, by default the default
// allocator. If dir is "" and $TMPDIR is not valid UTF-8, the path
// returned has U+FFFD for each invalid byte (AppendMkdirTemp returns the
// exact bytes).
func MkdirTemp(dir, pattern string, a alloc.Allocator = alloc.Default()) (string, error) {
	var buf [maxPath]byte
	b := vec.NewFixed(buf[:])
	if err := AppendMkdirTemp(&b, dir, pattern, a); err != nil {
		return "", err
	}
	return rt.StringFromBytes(&buf[0], b.Len(), a), nil
}

// AppendMkdirTemp is MkdirTemp appending the path to dst, a *vec.Vec[byte]
// (which grows) or a *vec.Fixed[byte], instead of making a new string: it
// allocates only for an error or to grow a Vec. A path that does not fit in
// a Fixed is ENAMETOOLONG, before anything is created, and dst is left as
// it was. The path is the exact bytes, which differ from a string only when
// dir is "" and $TMPDIR is not valid UTF-8.
func AppendMkdirTemp[D vec.Appender[byte]](dst D, dir, pattern string, a alloc.Allocator = alloc.Default()) error {
	dirb := strBytes(dir)
	if dir == "" {
		dirb = TempDirBytes()
	}
	prefix, suffix := pattern, ""
	for i := len(pattern) - 1; i >= 0; i-- {
		if pattern[i] == '/' {
			return NewPathError("mkdirtemp", pattern, ErrPatternHasSeparator, a)
		}
		if pattern[i] == '*' {
			prefix, suffix = pattern[:i], pattern[i+1:]
			break
		}
	}
	if len(dirb)+1+len(prefix)+20+len(suffix) > maxPath {
		return NewPathErrorBytes("mkdirtemp", dirb, syscall.ENAMETOOLONG, a)
	}
	for try := 0; try < 10000; try++ {
		var rnd [8]byte
		syscall.Getrandom(rnd[:], 0)
		r := uint64(0)
		for _, b := range rnd {
			r = r<<8 | uint64(b)
		}
		var nbuf [maxPath]byte
		nf := vec.NewFixed(nbuf[:])
		nf.Append(dirb...)
		if len(dirb) > 0 && dirb[len(dirb)-1] != '/' {
			nf.Push('/')
		}
		vec.AppendString(&nf, prefix)
		for r > 0 {
			nf.Push(byte('0' + r%10))
			r /= 10
		}
		vec.AppendString(&nf, suffix)
		name := nf.Slice()
		// The path goes into dst before the directory is made, so that a
		// path that does not fit is reported before anything exists.
		if !appendAll(dst, name) {
			return NewPathErrorBytes("mkdirtemp", dirb, syscall.ENAMETOOLONG, a)
		}
		var pb [maxPath]byte
		p, err := cpathBytes(name, &pb)
		if err == nil {
			err = syscall.Mkdirat(syscall.AT_FDCWD, p, 0o700)
		}
		if err == nil {
			return nil
		}
		dst.Truncate(dst.Len() - len(name))
		if err != syscall.EEXIST {
			return NewPathErrorBytes("mkdirtemp", name, err, a)
		}
	}
	return NewPathErrorBytes("mkdirtemp", dirb, syscall.EEXIST, a)
}

// ErrPatternHasSeparator is reported by MkdirTemp for a pattern with a "/".
var ErrPatternHasSeparator = errors.New("pattern contains path separator")

// Pipe returns a connected pair of Files: reads from r return bytes
// written to w. The Files are allocated from a, as Open's are. Their
// descriptors are non-blocking: a task that reads an empty pipe, or
// writes a full one, parks in the scheduler's poller and leaves its worker
// free for others (Fd makes them blocking again).
func Pipe(a alloc.Allocator = alloc.Default()) (r *File, w *File, err error) {
	var p [2]int32
	if err := syscall.Pipe2(&p, syscall.O_NONBLOCK|syscall.O_CLOEXEC); err != nil {
		return nil, nil, NewPathError("pipe", "", err, a)
	}
	r = alloc.New[File](a)
	r.fd, r.name, r.poll = int(p[0]), "|0", rt.PollOpen(int(p[0]))
	r.nonblock.Store(true)
	w = alloc.New[File](a)
	w.fd, w.name, w.poll = int(p[1]), "|1", rt.PollOpen(int(p[1]))
	w.nonblock.Store(true)
	return r, w, nil
}

// Getpid returns the process ID.
func Getpid() int { return syscall.Getpid() }

// Getuid returns the user ID.
func Getuid() int { return syscall.Getuid() }

// LookupEnv returns the value of the environment variable key and whether
// it is set. The value is made a valid string as Getenv makes it;
// LookupEnvBytes returns the exact bytes.
func LookupEnv(key string) (string, bool) {
	v, ok := lookupEnv(strBytes(key))
	return lossyView(v), ok
}

// Environ returns the environment as "key=value" strings, views of the
// process's environment, in a vec.Vec allocated from a. An entry that is
// not valid UTF-8 is a copy with U+FFFD for each invalid byte;
// EnvironBytes returns the exact bytes.
func Environ(a alloc.Allocator = alloc.Default()) vec.Vec[string] {
	env := rt.Environ()
	out := vec.New[string](a)
	for i := 0; ; i++ {
		p := *(**byte)(unsafe.Add(env, i*8))
		if p == nil {
			return out
		}
		out.Push(lossyView(cbytes(p)))
	}
}

// EnvironBytes returns the environment as exact "key=value" byte slices,
// views of the process's environment (valid for the life of the program; do
// not modify them), in a vec.Vec allocated from a.
func EnvironBytes(a alloc.Allocator = alloc.Default()) vec.Vec[[]byte] {
	env := rt.Environ()
	out := vec.New[[]byte](a)
	for i := 0; ; i++ {
		p := *(**byte)(unsafe.Add(env, i*8))
		if p == nil {
			return out
		}
		out.Push(cbytes(p))
	}
}

// Args returns the command-line arguments, starting with the program name,
// as views of the process's arguments in a vec.Vec allocated from a. An
// argument that is not valid UTF-8 is a copy with U+FFFD for each invalid
// byte; ArgsBytes returns the exact bytes.
func Args(a alloc.Allocator = alloc.Default()) vec.Vec[string] {
	n := NArgs()
	out := vec.WithCapacity[string](n, a)
	for i := 0; i < n; i++ {
		out.Push(Arg(i))
	}
	return out
}

// ArgsBytes returns the command-line arguments as exact byte slices (see
// ArgBytes), in a vec.Vec allocated from a.
func ArgsBytes(a alloc.Allocator = alloc.Default()) vec.Vec[[]byte] {
	n := NArgs()
	out := vec.WithCapacity[[]byte](n, a)
	for i := 0; i < n; i++ {
		out.Push(ArgBytes(i))
	}
	return out
}
