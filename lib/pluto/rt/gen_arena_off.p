//pu:build !gencheck

package rt

import "unsafe"

func GenCheckArenaFree(p unsafe.Pointer, size uintptr) {}

func GenTagArena(token uintptr, p unsafe.Pointer, size uintptr) unsafe.Pointer { return p }
func GenEndArena(token uintptr, pc uintptr)                                    {}
func arenaEndedSite(p unsafe.Pointer) uintptr                                  { return 0 }

func GenArenaExhausted(p unsafe.Pointer, size uintptr) uintptr { return 0 }
