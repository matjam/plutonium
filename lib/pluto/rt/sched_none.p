//pu:build none

package rt

// Freestanding builds have no backend of their own: the program binds one
// with //pu:scheduler (a kernel's, abi.md section 12).

// flushStackCache has nothing to do: backends own their stacks.
func flushStackCache() {}
