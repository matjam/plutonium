package syscall

import (
	"pluto/errors"
	"pluto/internal/oserror"
)

// An Errno is a Linux error number. It fits in an interface's data word, so
// returning one as an error never allocates and never refers to a stack
// frame. The usual way to check one is a comparison, err == syscall.ENOENT,
// or errors.Is with a portable error such as os.ErrNotExist.
type Errno uintptr

// Error returns the error's message, as Linux's strerror does in the C
// locale, in lower case as Go prints it.
func (e Errno) Error() string {
	if e < Errno(len(errnoTable)) && errnoTable[e].msg != "" {
		return errnoTable[e].msg
	}
	return "unknown error"
}

// Name returns the error's symbolic name, such as "ENOENT", or "" if it is
// not one this package knows.
func (e Errno) Name() string {
	if e < Errno(len(errnoTable)) {
		return errnoTable[e].name
	}
	return ""
}

// Is reports whether e matches one of the portable errors in pluto/os, so
// that errors.Is(err, os.ErrNotExist) is true for ENOENT.
func (e Errno) Is(target error) bool {
	switch target {
	case oserror.ErrPermission:
		return e == EACCES || e == EPERM
	case oserror.ErrExist:
		return e == EEXIST || e == ENOTEMPTY
	case oserror.ErrNotExist:
		return e == ENOENT
	case errors.ErrUnsupported:
		return e == ENOSYS || e == EOPNOTSUPP
	}
	return false
}

// Temporary reports whether retrying the operation may succeed.
func (e Errno) Temporary() bool {
	return e == EINTR || e == EMFILE || e == ENFILE || e.Timeout()
}

// Timeout reports whether the error is a timeout.
func (e Errno) Timeout() bool {
	return e == EAGAIN || e == ETIMEDOUT
}

// Error numbers (x86-64 Linux).
const (
	EPERM           Errno = 1
	ENOENT          Errno = 2
	ESRCH           Errno = 3
	EINTR           Errno = 4
	EIO             Errno = 5
	ENXIO           Errno = 6
	E2BIG           Errno = 7
	ENOEXEC         Errno = 8
	EBADF           Errno = 9
	ECHILD          Errno = 10
	EAGAIN          Errno = 11
	ENOMEM          Errno = 12
	EACCES          Errno = 13
	EFAULT          Errno = 14
	ENOTBLK         Errno = 15
	EBUSY           Errno = 16
	EEXIST          Errno = 17
	EXDEV           Errno = 18
	ENODEV          Errno = 19
	ENOTDIR         Errno = 20
	EISDIR          Errno = 21
	EINVAL          Errno = 22
	ENFILE          Errno = 23
	EMFILE          Errno = 24
	ENOTTY          Errno = 25
	ETXTBSY         Errno = 26
	EFBIG           Errno = 27
	ENOSPC          Errno = 28
	ESPIPE          Errno = 29
	EROFS           Errno = 30
	EMLINK          Errno = 31
	EPIPE           Errno = 32
	EDOM            Errno = 33
	ERANGE          Errno = 34
	EDEADLK         Errno = 35
	ENAMETOOLONG    Errno = 36
	ENOLCK          Errno = 37
	ENOSYS          Errno = 38
	ENOTEMPTY       Errno = 39
	ELOOP           Errno = 40
	EWOULDBLOCK     Errno = EAGAIN
	ENOMSG          Errno = 42
	EIDRM           Errno = 43
	ENODATA         Errno = 61
	ETIME           Errno = 62
	EPROTO          Errno = 71
	EOVERFLOW       Errno = 75
	EILSEQ          Errno = 84
	ENOTSOCK        Errno = 88
	EDESTADDRREQ    Errno = 89
	EMSGSIZE        Errno = 90
	EPROTOTYPE      Errno = 91
	ENOPROTOOPT     Errno = 92
	EPROTONOSUPPORT Errno = 93
	ESOCKTNOSUPPORT Errno = 94
	EOPNOTSUPP      Errno = 95
	ENOTSUP         Errno = EOPNOTSUPP
	EPFNOSUPPORT    Errno = 96
	EAFNOSUPPORT    Errno = 97
	EADDRINUSE      Errno = 98
	EADDRNOTAVAIL   Errno = 99
	ENETDOWN        Errno = 100
	ENETUNREACH     Errno = 101
	ENETRESET       Errno = 102
	ECONNABORTED    Errno = 103
	ECONNRESET      Errno = 104
	ENOBUFS         Errno = 105
	EISCONN         Errno = 106
	ENOTCONN        Errno = 107
	ESHUTDOWN       Errno = 108
	ETOOMANYREFS    Errno = 109
	ETIMEDOUT       Errno = 110
	ECONNREFUSED    Errno = 111
	EHOSTDOWN       Errno = 112
	EHOSTUNREACH    Errno = 113
	EALREADY        Errno = 114
	EINPROGRESS     Errno = 115
	ESTALE          Errno = 116
	EDQUOT          Errno = 122
	ECANCELED       Errno = 125
)

type errnoInfo struct {
	name, msg string
}

// errnoTable holds the name and message of each known error number.
var errnoTable = [...]errnoInfo{
	EPERM:           {"EPERM", "operation not permitted"},
	ENOENT:          {"ENOENT", "no such file or directory"},
	ESRCH:           {"ESRCH", "no such process"},
	EINTR:           {"EINTR", "interrupted system call"},
	EIO:             {"EIO", "input/output error"},
	ENXIO:           {"ENXIO", "no such device or address"},
	E2BIG:           {"E2BIG", "argument list too long"},
	ENOEXEC:         {"ENOEXEC", "exec format error"},
	EBADF:           {"EBADF", "bad file descriptor"},
	ECHILD:          {"ECHILD", "no child processes"},
	EAGAIN:          {"EAGAIN", "resource temporarily unavailable"},
	ENOMEM:          {"ENOMEM", "cannot allocate memory"},
	EACCES:          {"EACCES", "permission denied"},
	EFAULT:          {"EFAULT", "bad address"},
	ENOTBLK:         {"ENOTBLK", "block device required"},
	EBUSY:           {"EBUSY", "device or resource busy"},
	EEXIST:          {"EEXIST", "file exists"},
	EXDEV:           {"EXDEV", "invalid cross-device link"},
	ENODEV:          {"ENODEV", "no such device"},
	ENOTDIR:         {"ENOTDIR", "not a directory"},
	EISDIR:          {"EISDIR", "is a directory"},
	EINVAL:          {"EINVAL", "invalid argument"},
	ENFILE:          {"ENFILE", "too many open files in system"},
	EMFILE:          {"EMFILE", "too many open files"},
	ENOTTY:          {"ENOTTY", "inappropriate ioctl for device"},
	ETXTBSY:         {"ETXTBSY", "text file busy"},
	EFBIG:           {"EFBIG", "file too large"},
	ENOSPC:          {"ENOSPC", "no space left on device"},
	ESPIPE:          {"ESPIPE", "illegal seek"},
	EROFS:           {"EROFS", "read-only file system"},
	EMLINK:          {"EMLINK", "too many links"},
	EPIPE:           {"EPIPE", "broken pipe"},
	EDOM:            {"EDOM", "numerical argument out of domain"},
	ERANGE:          {"ERANGE", "numerical result out of range"},
	EDEADLK:         {"EDEADLK", "resource deadlock avoided"},
	ENAMETOOLONG:    {"ENAMETOOLONG", "file name too long"},
	ENOLCK:          {"ENOLCK", "no locks available"},
	ENOSYS:          {"ENOSYS", "function not implemented"},
	ENOTEMPTY:       {"ENOTEMPTY", "directory not empty"},
	ELOOP:           {"ELOOP", "too many levels of symbolic links"},
	ENOMSG:          {"ENOMSG", "no message of desired type"},
	EIDRM:           {"EIDRM", "identifier removed"},
	ENODATA:         {"ENODATA", "no data available"},
	ETIME:           {"ETIME", "timer expired"},
	EPROTO:          {"EPROTO", "protocol error"},
	EOVERFLOW:       {"EOVERFLOW", "value too large for defined data type"},
	EILSEQ:          {"EILSEQ", "invalid or incomplete multibyte or wide character"},
	ENOTSOCK:        {"ENOTSOCK", "socket operation on non-socket"},
	EDESTADDRREQ:    {"EDESTADDRREQ", "destination address required"},
	EMSGSIZE:        {"EMSGSIZE", "message too long"},
	EPROTOTYPE:      {"EPROTOTYPE", "protocol wrong type for socket"},
	ENOPROTOOPT:     {"ENOPROTOOPT", "protocol not available"},
	EPROTONOSUPPORT: {"EPROTONOSUPPORT", "protocol not supported"},
	ESOCKTNOSUPPORT: {"ESOCKTNOSUPPORT", "socket type not supported"},
	EOPNOTSUPP:      {"EOPNOTSUPP", "operation not supported"},
	EPFNOSUPPORT:    {"EPFNOSUPPORT", "protocol family not supported"},
	EAFNOSUPPORT:    {"EAFNOSUPPORT", "address family not supported by protocol"},
	EADDRINUSE:      {"EADDRINUSE", "address already in use"},
	EADDRNOTAVAIL:   {"EADDRNOTAVAIL", "cannot assign requested address"},
	ENETDOWN:        {"ENETDOWN", "network is down"},
	ENETUNREACH:     {"ENETUNREACH", "network is unreachable"},
	ENETRESET:       {"ENETRESET", "network dropped connection on reset"},
	ECONNABORTED:    {"ECONNABORTED", "software caused connection abort"},
	ECONNRESET:      {"ECONNRESET", "connection reset by peer"},
	ENOBUFS:         {"ENOBUFS", "no buffer space available"},
	EISCONN:         {"EISCONN", "transport endpoint is already connected"},
	ENOTCONN:        {"ENOTCONN", "transport endpoint is not connected"},
	ESHUTDOWN:       {"ESHUTDOWN", "cannot send after transport endpoint shutdown"},
	ETOOMANYREFS:    {"ETOOMANYREFS", "too many references: cannot splice"},
	ETIMEDOUT:       {"ETIMEDOUT", "connection timed out"},
	ECONNREFUSED:    {"ECONNREFUSED", "connection refused"},
	EHOSTDOWN:       {"EHOSTDOWN", "host is down"},
	EHOSTUNREACH:    {"EHOSTUNREACH", "no route to host"},
	EALREADY:        {"EALREADY", "operation already in progress"},
	EINPROGRESS:     {"EINPROGRESS", "operation now in progress"},
	ESTALE:          {"ESTALE", "stale NFS file handle"},
	EDQUOT:          {"EDQUOT", "disk quota exceeded"},
	ECANCELED:       {"ECANCELED", "operation canceled"},
}
