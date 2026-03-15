package syscall

// Errno is a syscall error number.
// The concrete values are OS-specific; this type is arch/OS neutral.
type Errno uintptr

func (e Errno) Error() string {
	// TODO: error string table per OS
	return "syscall error"
}

// Syscall performs a raw OS syscall with up to 3 arguments.
// Returns r1, r2, errno.
// Implemented in OS+arch specific assembly stubs.
func Syscall(num, a1, a2, a3 uintptr) (r1, r2, err uintptr)

// Syscall6 performs a raw OS syscall with up to 6 arguments.
// Implemented in OS+arch specific assembly stubs.
func Syscall6(num, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2, err uintptr)
