//pu:build gencheck

package rt

import "unsafe"

// The generation directory's ordered accesses on x86-64 (abi.md section
// 1.3; memory-model section 3.8). Readers load record versions, fields,
// links, hints and table entries that writers change concurrently;
// writers (serialized by genWriter) store tags, hints and cleared tables.
// The protocol needs each load to be an acquire and each store a release,
// and each access to be one indivisible access of its width.
//
// A naturally aligned volatile access is one MOV of exactly its width, and
// the compiler moves no other memory access across it (memory-model
// sections 4.4 and 5.2). The language gives volatile accesses no ordering
// between processors; the acquire and release semantics come from the
// hardware: under x86-64 total store order a MOV load is an acquire and a
// MOV store a release (memory-model section 5.3). That is why these
// helpers live in an amd64 file. The protocol has no store-then-load
// pattern between processors, so no fence is needed. Seqlock versions and
// directory publication use store64, a sequentially consistent exchange.
//
// A weakly ordered target supplies these four with acquire loads and
// release stores (gen_order_other.p).

//pu:inline
func vload64(p *uint64) uint64 { return *(*volatile[uint64])(unsafe.Pointer(p)) }

//pu:inline
func vload16(p *uint16) uint16 { return *(*volatile[uint16])(unsafe.Pointer(p)) }

//pu:inline
func vstore64(p *uint64, v uint64) { *(*volatile[uint64])(unsafe.Pointer(p)) = v }

//pu:inline
func vstore16(p *uint16, v uint16) { *(*volatile[uint16])(unsafe.Pointer(p)) = v }
