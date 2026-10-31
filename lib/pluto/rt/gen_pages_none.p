//pu:build gencheck && none

package rt

import "unsafe"

// In a freestanding or kernel build there is no system to map memory, so
// the program supplies the memory for generation metadata (abi.md section
// 1.3). It exports a function under the symbol pluto_gen_metadata:
//
//	//pu:export pluto_gen_metadata
//	func genMetadataPages(n uintptr) uintptr
//
// which returns the address of n bytes that the runtime keeps for good, or
// 0 when it has none. The symbol binds the source when the program is
// linked, and a checked build that does not export it fails to build
// (E2588). The runtime asks for 64 KiB at a time, and 512 KiB for each
// directory leaf, while it holds the directory's writer lock, so the
// function must not allocate from the heap. The runtime zeroes the memory.

//pu:extern pluto_gen_metadata
func programGenMetadata(n uintptr) uintptr

// genPages returns n bytes of zeroed memory for generation metadata from
// the program's source, or nil.
func genPages(n uintptr) unsafe.Pointer {
	p := programGenMetadata(n)
	if p == 0 {
		return nil
	}
	memclr(unsafe.Pointer(p), int(n))
	return unsafe.Pointer(p)
}

// The tags take bits 63..48 of a pointer, which only 4-level paging leaves
// free: with 5-level paging (LA57) addresses use bits 56..48 too. So a
// checked program refuses to start under 5-level paging, before anything is
// allocated (decision D41).
func init() {
	if fiveLevelPaging() {
		panic("generation checks need 4-level paging, and this processor runs with 5-level paging (CR4.LA57); build with --gencheck=false to run unchecked")
	}
}

// fiveLevelPaging reports whether the processor runs with 5-level paging
// (gen_paging_none_amd64.s). Code outside ring 0 cannot read CR4 and
// reports false: its address space is its host's.
func fiveLevelPaging() bool
