//pu:build none

package rt

// Freestanding builds have no backend of their own: the program binds one
// with //pu:scheduler (a kernel's, abi.md section 12).

// flushStackCache has nothing to do: backends own their stacks.
func flushStackCache() {}

func flushShutdownCaches() {}

// Timers use the shared heap, which the program runs with RunTimers when
// its backend's ArmTimer deadline has passed (abi.md section 12.9).

func timerStart() {}

func timerHeapLocal() *timerHeap { return &globalTimers }

func timerEarlier(h *timerHeap, when int64) { armGlobal(when) }

// sleepDirect is never chosen here: the program's own backend is bound, so
// Sleep always parks (it exists so that Sleep type-checks in every build).
func sleepDirect(ns int64) { sleepParked(ns) }
