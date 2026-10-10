package debug

import (
	"pluto/rt"
	"unsafe"
)

// The side table: records live in d.recs, indexed by number, and d.table
// maps an allocation's address to its record by linear probing. Both grow
// by doubling, in memory taken straight from the page provider.

// newRecord returns the number of an unused record, growing the records if
// none is free.
func (d *Allocator) newRecord() int32 {
	if d.freeRecs >= 0 {
		ri := d.freeRecs
		d.freeRecs = d.recs[ri].next
		return ri
	}
	if d.nrecs == len(d.recs) {
		d.growRecords()
	}
	ri := int32(d.nrecs)
	d.nrecs++
	return ri
}

// growRecords doubles the record array (or creates it).
func (d *Allocator) growRecords() {
	n := 2 * len(d.recs)
	if n == 0 {
		n = 1024
	}
	if n > 1<<31-1 {
		panic("debug allocator: too many allocations")
	}
	p := d.pages.AllocPages(pagesFor(uintptr(n) * unsafe.Sizeof(record{})))
	if p == nil {
		panic("debug allocator: out of memory for the allocation table")
	}
	recs := unsafe.Slice((*record)(p), n)
	copy(recs, d.recs)
	d.freeArray(unsafe.Pointer(unsafe.SliceData(d.recs)), uintptr(len(d.recs))*unsafe.Sizeof(record{}))
	d.recs = recs
}

// growTable doubles the hash table (or creates it) and reinserts every
// record in use.
func (d *Allocator) growTable() {
	n := 2 * len(d.table)
	if n == 0 {
		n = 4096
	}
	p := d.pages.AllocPages(pagesFor(uintptr(n) * 4))
	if p == nil {
		panic("debug allocator: out of memory for the allocation table")
	}
	d.freeArray(unsafe.Pointer(unsafe.SliceData(d.table)), uintptr(len(d.table))*4)
	d.table = unsafe.Slice((*uint32)(p), n)
	d.shift = 64
	for m := n; m > 1; m >>= 1 {
		d.shift--
	}
	d.used = 0
	for i := 0; i < d.nrecs; i++ {
		if d.recs[i].state != stateUnused {
			d.place(int32(i))
		}
	}
}

func (d *Allocator) freeArray(p unsafe.Pointer, bytes uintptr) {
	if p != nil && bytes > 0 {
		d.pages.FreePages(p, pagesFor(bytes))
	}
}

// home returns the table slot an address hashes to.
func (d *Allocator) home(ptr uintptr) int {
	return int((uint64(ptr) * 0x9E3779B97F4A7C15) >> d.shift)
}

// insert adds record ri, whose ptr is set, to the table.
func (d *Allocator) insert(ri int32) {
	if 2*(d.used+1) > len(d.table) {
		d.growTable() // places every record in use, ri among them
		return
	}
	d.place(ri)
}

func (d *Allocator) place(ri int32) {
	mask := len(d.table) - 1
	i := d.home(d.recs[ri].ptr)
	for d.table[i] != 0 {
		i = (i + 1) & mask
	}
	d.table[i] = uint32(ri) + 1
	d.used++
}

// lookup returns the record for the allocation at ptr, live or in
// quarantine, or -1.
func (d *Allocator) lookup(ptr uintptr) int32 {
	mask := len(d.table) - 1
	for i := d.home(ptr); d.table[i] != 0; i = (i + 1) & mask {
		ri := int32(d.table[i] - 1)
		if d.recs[ri].ptr == ptr {
			return ri
		}
	}
	return -1
}

// remove deletes record ri from the table, shifting later entries of its
// probe run back so lookups need no tombstones.
func (d *Allocator) remove(ri int32) {
	mask := len(d.table) - 1
	i := d.home(d.recs[ri].ptr)
	for d.table[i] != uint32(ri)+1 {
		i = (i + 1) & mask
	}
	j := i
	for {
		j = (j + 1) & mask
		if d.table[j] == 0 {
			break
		}
		h := d.home(d.recs[d.table[j]-1].ptr)
		if (j > i && (h <= i || h > j)) || (j < i && h <= i && h > j) {
			d.table[i] = d.table[j]
			i = j
		}
	}
	d.table[i] = 0
	d.used--
}

// ----------------------------------------------------------------------------
// Memory helpers

func pagesFor(n uintptr) uintptr {
	if n == 0 {
		return 1
	}
	return (n + pageSize - 1) / pageSize
}

func alignUp(p, align uintptr) uintptr { return (p + align - 1) &^ (align - 1) }

// fill sets n bytes at p to b.
func fill(p, n uintptr, b byte) {
	if n == 0 {
		return
	}
	s := unsafe.Slice((*byte)(unsafe.Pointer(p)), n)
	for i := range s {
		s[i] = b
	}
}

// findNot returns the address of the first of n bytes at p that is not b,
// or 0 if they all are.
func findNot(p, n uintptr, b byte) uintptr {
	if n == 0 {
		return 0
	}
	s := unsafe.Slice((*byte)(unsafe.Pointer(p)), n)
	for i := range s {
		if s[i] != b {
			return p + uintptr(i)
		}
	}
	return 0
}

// ----------------------------------------------------------------------------
// Messages. They are printed piece by piece, since the allocator cannot
// allocate to format them, and end with rt.Abort.

// failHead starts a fatal message.
func failHead(what string) {
	print("panic: debug allocator: ")
	print(what)
}

func printDec(v uintptr) { print(uint64(v)) }

func printHex(v uintptr) {
	const digits = "0123456789abcdef"
	var buf [18]byte
	i := len(buf)
	for {
		i--
		buf[i] = digits[v&15]
		v >>= 4
		if v == 0 {
			break
		}
	}
	i--
	buf[i] = 'x'
	i--
	buf[i] = '0'
	print(unsafe.String(&buf[i], len(buf)-i))
}

// printRet prints the site of a call given its return address: the
// function and line of the call, or its address if rt.PrintPC cannot name
// it.
func printRet(ret uintptr) {
	if ret == 0 {
		print("an unknown site")
		return
	}
	rt.PrintPC(ret - 1)
}

// printSite prints a labelled line naming the call that returns to ret.
func printSite(label string, ret uintptr) {
	print(label)
	printRet(ret)
	print("\n")
}

// printRecordSites prints where r was allocated and, if it was, freed.
func printRecordSites(r *record) {
	printSite("  allocated at ", r.pc)
	if r.state == stateFreed {
		printSite("  freed at ", r.freePC)
	}
}
