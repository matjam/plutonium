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

// Pages is the hosted page provider, backed by mmap.
var Pages PageProvider = mmapPages{}

func init() {
	SetDefault(NewSlab(Pages))
}
