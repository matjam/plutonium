//pu:build linux

package alloc

import (
	"pluto/rt"
	"unsafe"
)

// mmapPages is the hosted page provider.
type mmapPages struct{}

func (mmapPages) AllocPages(n uintptr) unsafe.Pointer { return rt.Mmap(n * PageSize) }

func (mmapPages) FreePages(p unsafe.Pointer, n uintptr) { rt.Munmap(p, n*PageSize) }

// ProtectPages makes n pages at p readable and writable, or inaccessible.
// The debug allocator uses it for guard pages and freed memory.
func (mmapPages) ProtectPages(p unsafe.Pointer, n uintptr, access bool) bool {
	prot := rt.ProtNone
	if access {
		prot = rt.ProtRead | rt.ProtWrite
	}
	return rt.Mprotect(p, n*PageSize, prot)
}

// Pages is the hosted page provider, backed by mmap.
var Pages PageProvider = mmapPages{}

func init() {
	SetDefault(NewSlab(Pages))
}
