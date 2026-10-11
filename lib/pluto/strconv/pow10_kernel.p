//pu:build kernel

package strconv

// ParseFloat is not available in kernel builds, which may not use floating
// point (language spec section 20.7) and have no table of powers of ten.
// Kernel code that reaches it fails to compile because of its float
// arithmetic; this stub only lets the package type-check.
func pow10(p int) (hi, lo uint64) {
	panic("strconv: ParseFloat is not available in kernel builds")
}
