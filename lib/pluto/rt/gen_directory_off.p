//pu:build !gencheck

package rt

import "unsafe"

func GenRegisteredLive(p unsafe.Pointer) bool                 { return true }
func GenRegisteredRangeLive(p unsafe.Pointer, n uintptr) bool { return true }
func GenRegister(p unsafe.Pointer, size uintptr, backing unsafe.Pointer, key uintptr) uintptr {
	return 0
}
func GenUnregister(token uintptr)                                             {}
func GenTagHeap(token uintptr, p unsafe.Pointer, size uintptr) unsafe.Pointer { return p }
func GenFreeHeap(token uintptr, p unsafe.Pointer, size uintptr) bool          { return true }
func GenInvalidate(token uintptr, from, to uintptr)                           {}
func GenCustomAlloc(p unsafe.Pointer, size, key uintptr) unsafe.Pointer       { return p }
func GenCustomFree(p unsafe.Pointer, size, key uintptr) unsafe.Pointer        { return p }

func GenAfterCustomFree(p unsafe.Pointer, size uintptr) {}

func GenQuarantine(token uintptr) {}

func GenSlabOwner(token, owner uintptr)                       {}
func GenSlabBackingLive(p unsafe.Pointer, owner uintptr) bool { return true }

func GenNewSlabOwner() uintptr { return 0 }
