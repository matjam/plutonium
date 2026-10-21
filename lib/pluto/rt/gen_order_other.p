//pu:build gencheck && !amd64

package rt

// The generation directory's ordered accesses on a target other than
// x86-64 (see gen_order_amd64.p). Volatile accesses do not order memory
// between processors there (memory-model sections 4.4 and 5.3), so the
// port must implement these in assembly as acquire loads and release
// stores (LDAR and STLR on arm64) of exactly the given width. Until it
// does, a checked build does not link.

//pu:noescape
func vload64(p *uint64) uint64

//pu:noescape
func vload16(p *uint16) uint16

//pu:noescape
func vstore64(p *uint64, v uint64)

//pu:noescape
func vstore16(p *uint16, v uint16)
