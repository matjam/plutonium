//pu:build gencheck && linux

package rt

import "unsafe"

// genPages returns n bytes of zeroed memory for generation metadata, or
// nil: anonymous memory from the system.
func genPages(n uintptr) unsafe.Pointer { return Mmap(n) }
