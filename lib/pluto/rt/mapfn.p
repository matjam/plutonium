package rt

import "unsafe"

// Hash functions for map[K]V and pluto/hash (which re-exports them): fast
// mixing functions in the style of wyhash, not cryptographic.

// Mixing constants from wyhash (public domain).
const (
	wyp0 = 0xa0761d6478bd642f
	wyp1 = 0xe7037ed1a0b428db
	wyp2 = 0x8ebc6af09c88c6e3
)

// mul64 returns the 128-bit product of x and y: (hi, lo) = x * y. The
// compiler replaces calls with one multiply instruction.
func mul64(x, y uint64) (hi, lo uint64) {
	const mask32 = 1<<32 - 1
	x0 := x & mask32
	x1 := x >> 32
	y0 := y & mask32
	y1 := y >> 32
	w0 := x0 * y0
	t := x1*y0 + w0>>32
	w1 := t & mask32
	w2 := t >> 32
	w1 += x0 * y1
	hi = x1*y1 + w2 + w1>>32
	lo = x * y
	return
}

// mix multiplies a and b to 128 bits and folds the halves together.
//
//pu:inline
func mix(a, b uint64) uint64 {
	hi, lo := mul64(a, b)
	return hi ^ lo
}

// loadLE64 reads 8 bytes at p+i, little-endian, without alignment.
//
//pu:inline
func loadLE64(p unsafe.Pointer, i int) uint64 {
	b := (*[8]byte)(unsafe.Add(p, i))
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

// loadLE32 reads 4 bytes at p+i, little-endian, without alignment.
//
//pu:inline
func loadLE32(p unsafe.Pointer, i int) uint64 {
	b := (*[4]byte)(unsafe.Add(p, i))
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24
}

// MemHash returns a 64-bit hash of the n bytes at p.
func MemHash(p unsafe.Pointer, n int, seed uint64) uint64 {
	seed ^= wyp0
	var a, b uint64
	switch {
	case n == 0:
		return mix(seed^wyp1, wyp1)
	case n < 4:
		q := (*[3]byte)(p)
		a = uint64(q[0])<<16 | uint64(q[n>>1])<<8 | uint64(q[n-1])
	case n <= 8:
		a = loadLE32(p, 0)
		b = loadLE32(p, n-4)
	case n <= 16:
		a = loadLE64(p, 0)
		b = loadLE64(p, n-8)
	default:
		i := 0
		for ; n-i > 16; i += 16 {
			seed = mix(loadLE64(p, i)^wyp1, loadLE64(p, i+8)^seed)
		}
		a = loadLE64(p, n-16)
		b = loadLE64(p, n-8)
	}
	return mix(wyp1^uint64(n), mix(a^wyp1, b^seed))
}

// StringHash returns a 64-bit hash of s. It equals MemHash of its bytes.
func StringHash(s string, seed uint64) uint64 {
	return MemHash(unsafe.Pointer(unsafe.StringData(s)), len(s), seed)
}

// Uint64Hash returns a 64-bit hash of x.
//
//pu:inline
func Uint64Hash(x, seed uint64) uint64 {
	return mix(mix(x^wyp0, seed^wyp1), wyp2^8)
}

// FloatHash returns a 64-bit hash of a float64 (size 8) or float32 (size
// 4) given by its bits, giving 0 and -0 the same hash, as they compare
// equal. NaNs hash to arbitrary values (a NaN is not equal to itself, so a
// map can never find one again). It uses integer instructions only, so
// that maps work in kernel code, where floats need //pu:fpu.
//
//pu:inline
func FloatHash(bits uint64, size uintptr, seed uint64) uint64 {
	if bits<<(65-8*size) == 0 {
		bits = 0 // -0 becomes +0: only the sign bit was set
	}
	return Uint64Hash(bits, seed)
}

// hashAt hashes the value of type t at p by walking its descriptor: the
// hash of keys a map cannot hash as plain memory (structs and arrays with
// strings, floats, interfaces or padding in them, and interface values).
// Values that compare equal hash equally: blank fields are skipped, -0 and
// +0 are the same, and an interface hashes its dynamic value. It panics,
// as Go does, for an interface holding a value of a type that cannot be
// compared.
func hashAt(t *typeDesc, p unsafe.Pointer, seed uint64) uint64 {
	switch t.kind {
	case KindFloat32:
		return FloatHash(uint64(*(*uint32)(p)), 4, seed)
	case KindFloat64:
		return FloatHash(*(*uint64)(p), 8, seed)
	case KindString:
		return StringHash(*(*string)(p), seed)
	case KindInterface:
		dt, w := t.IfaceAt(p)
		if dt == nil {
			return Uint64Hash(0, seed)
		}
		if dt.flags&flagDirect == 0 && dt.equal == nil {
			panicMessage("runtime error: hash of unhashable type ", dt.name, "", "", "")
		}
		seed = Uint64Hash(uint64(dt.hash), seed)
		if dt.flags&flagDirect != 0 {
			if dt.flags&flagSwapped != 0 {
				w = swapWord(w, dt.size)
			}
			return hashAt(dt, unsafe.Pointer(&w), seed)
		}
		return hashAt(dt, w, seed)
	case KindArray:
		for i := uintptr(0); i < t.len; i++ {
			seed = hashAt(t.elem, unsafe.Add(p, i*t.elem.size), seed)
		}
		return seed
	case KindStruct:
		for i := uintptr(0); i < t.len; i++ {
			f := (*fieldDesc)(unsafe.Add(unsafe.Pointer(t.fields), i*unsafe.Sizeof(fieldDesc{})))
			if f.flags&fieldBlank == 0 {
				seed = hashAt(f.typ, unsafe.Add(p, f.offset), seed)
			}
		}
		return seed
	case KindPacked:
		if t.fields != nil {
			return maskedHash(p, (*byte)(unsafe.Pointer(t.fields)), int(t.size), seed)
		}
	case KindUnion:
		// Equal unions hold the same variant; hashing only the tag keeps
		// equal values' hashes equal.
		tag := uint64(*(*uint8)(p))
		if (*unionDesc)(unsafe.Pointer(t)).count > 256 {
			tag = uint64(*(*uint16)(p))
		}
		return Uint64Hash(tag, seed)
	}
	switch t.size {
	case 1:
		return Uint64Hash(uint64(*(*uint8)(p)), seed)
	case 2:
		return Uint64Hash(uint64(*(*uint16)(p)), seed)
	case 4:
		return Uint64Hash(uint64(*(*uint32)(p)), seed)
	case 8:
		return Uint64Hash(*(*uint64)(p), seed)
	}
	return MemHash(p, int(t.size), seed)
}

// maskedHash hashes the n bytes at p ANDed with the n bytes at mask, so that
// packed values that differ only in their reserved bits (spec section 7.14)
// hash alike.
func maskedHash(p unsafe.Pointer, mask *byte, n int, seed uint64) uint64 {
	for i := 0; i < n; i += 8 {
		var w uint64
		for j := 0; j < 8 && i+j < n; j++ {
			b := *(*byte)(unsafe.Add(p, i+j)) & *(*byte)(unsafe.Add(unsafe.Pointer(mask), i+j))
			w |= uint64(b) << (8 * j)
		}
		seed = Uint64Hash(w, seed)
	}
	return seed
}

// plainMemory reports whether values of type t compare equal exactly when
// their bytes do, so that a map can hash their bytes: integers, booleans
// and pointers, and arrays and structs of them with no padding and no
// blank fields.
func plainMemory(t *typeDesc) bool {
	switch t.kind {
	case KindBool, KindInt, KindInt8, KindInt16, KindInt32, KindInt64,
		KindUint, KindUint8, KindUint16, KindUint32, KindUint64, KindUintptr,
		KindPointer, KindUnsafePointer, KindChan, KindOwned:
		return true
	case KindArray:
		return plainMemory(t.elem)
	case KindStruct:
		end := uintptr(0)
		for i := uintptr(0); i < t.len; i++ {
			f := (*fieldDesc)(unsafe.Add(unsafe.Pointer(t.fields), i*unsafe.Sizeof(fieldDesc{})))
			if f.offset != end || f.flags&fieldBlank != 0 || !plainMemory(f.typ) {
				return false
			}
			end += f.typ.size
		}
		return end == t.size
	}
	return false
}
