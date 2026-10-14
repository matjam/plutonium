//pu:build !gencheck

package rt

import "unsafe"

func GenTrimArena(token, from, pc uintptr) {}

func GenCheckArenaFree(p unsafe.Pointer, size uintptr) {}

func GenTagArena(token uintptr, p unsafe.Pointer, size uintptr) unsafe.Pointer { return p }
func GenEndArena(token uintptr, pc uintptr, freed bool)                        {}
func arenaEndedSite(p unsafe.Pointer) uintptr                                  { return 0 }
