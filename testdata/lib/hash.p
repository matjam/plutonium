package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/hash"
	"pluto/strconv"
	"pluto/vec"
	"unsafe"
)

var seed uint64 = 99

// The keys of the maps below that are views live in package variables: a
// map's table is heap memory, which may not hold views of a frame (language
// spec section 18.3).
var (
	store [1 << 16]byte
	objs  [10]int
)

func rnd() uint64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return seed >> 33
}

type counting struct {
	inner alloc.Allocator
	live  int
}

func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.live += int(size)
	return c.inner.Alloc(size, align)
}

func (c *counting) Free(p unsafe.Pointer, size, align uintptr) {
	c.live -= int(size)
	c.inner.Free(p, size, align)
}

type ID int32

type point struct{ x, y int }

func hashPoint(p point) uint64 { return hash.Uint64(uint64(p.x)*31+uint64(p.y), 1) }

type name struct{ s string }

func (n name) Hash(seed uint64) uint64 { return hash.String(n.s, seed) }

func main() {
	ca := &counting{inner: alloc.Default()}
	// Randomised check against a direct-address reference.
	const N = 4096
	var ref [N]int
	var present [N]bool
	m := hash.NewMap[int, int](0, ca)
	count := 0
	for step := 0; step < 200000; step++ {
		k := int(rnd() % N)
		switch rnd() % 3 {
		case 0, 1:
			v := int(rnd())
			m.Set(k, v)
			if !present[k] {
				count++
			}
			ref[k], present[k] = v, true
		case 2:
			ok := m.Delete(k)
			if ok != present[k] {
				fmt.Printf("delete mismatch at step %d key %d\n", step, k)
				return
			}
			if present[k] {
				count--
			}
			present[k] = false
		}
		if step%1000 == 0 {
			for j := 0; j < N; j++ {
				v, ok := m.Get(j)
				if ok != present[j] || ok && v != ref[j] {
					fmt.Printf("get mismatch at step %d key %d\n", step, j)
					return
				}
			}
		}
	}
	it := m.Iter()
	seen := 0
	for k, v, ok := it.Next(); ok; k, v, ok = it.Next() {
		if !present[k] || ref[k] != v {
			fmt.Printf("iter mismatch %d\n", k)
		}
		seen++
	}
	fmt.Printf("int map ok len=%d count=%d seen=%d\n", m.Len(), count, seen)
	m.Clear()
	fmt.Printf("cleared len=%d has=%t\n", m.Len(), m.Has(5))
	m.Free()
	fmt.Printf("freed live=%d\n", ca.live)

	// String keys built in a buffer.
	used := 0
	sm := hash.NewMap[string, int](16, ca)
	for i := 0; i < 2000; i++ {
		b := vec.NewFixed(store[used:])
		strconv.AppendInt(&b, int64(i), 10)
		s := unsafe.String(&store[used], b.Len())
		used += b.Len()
		sm.Set(s, i)
	}
	v1, ok1 := sm.Get("1234")
	v2, ok2 := sm.Get("9999")
	fmt.Printf("strings len=%d 1234=%d,%t 9999=%d,%t\n", sm.Len(), v1, ok1, v2, ok2)
	p, ins := sm.Upsert("1234")
	*p += 1
	p2, ins2 := sm.Upsert("new")
	*p2 = 7
	v1, _ = sm.Get("1234")
	v3, _ := sm.Get("new")
	fmt.Printf("upsert %t %t %d %d len=%d\n", ins, ins2, v1, v3, sm.Len())
	*sm.Ptr("new") = 8
	fmt.Printf("ptr %d nil=%t\n", *sm.Ptr("new"), sm.Ptr("nope") == nil)
	sm.Free()

	// Named integer keys, bool, float keys.
	im := hash.NewMap[ID, string](0, ca)
	im.Set(ID(-1), "minus one")
	im.Set(ID(1<<20), "big")
	s1, _ := im.Get(-1)
	s2, _ := im.Get(1 << 20)
	_, ok3 := im.Get(2)
	fmt.Printf("%s %s %t\n", s1, s2, ok3)
	im.Free()
	fm := hash.NewMap[float64, int](0, ca)
	negz := 0.0
	negz = -negz
	fm.Set(0.0, 1)
	fm.Set(negz, 2)
	fm.Set(1.5, 3)
	f0, _ := fm.Get(0)
	fmt.Printf("floats len=%d zero=%d\n", fm.Len(), f0)
	fm.Free()
	bm := hash.NewMap[bool, int](0, ca)
	bm.Set(true, 1)
	bm.Set(false, 2)
	bt, _ := bm.Get(true)
	fmt.Printf("bool len=%d true=%d\n", bm.Len(), bt)
	bm.Free()

	// Pointer keys.
	pm := hash.NewMap[*int, int](0, ca)
	for i := range objs {
		pm.Set(&objs[i], i*10)
	}
	pv, _ := pm.Get(&objs[7])
	fmt.Printf("ptr keys %d %d\n", pm.Len(), pv)
	pm.Free()

	// Struct keys with a hash function, and with a Hash method.
	var pt hash.Map[point, int]
	pt.InitFunc(0, hashPoint, ca)
	for x := 0; x < 50; x++ {
		for y := 0; y < 50; y++ {
			pt.Set(point{x, y}, x*100+y)
		}
	}
	q, _ := pt.Get(point{12, 34})
	fmt.Printf("points %d %d\n", pt.Len(), q)
	pt.Free()
	var nm hash.Map[name, int]
	nm.Init(4, ca)
	nm.Set(name{"alice"}, 1)
	nm.Set(name{"bob"}, 2)
	nb, _ := nm.Get(name{"bob"})
	fmt.Printf("hasher %d %d\n", nm.Len(), nb)
	nm.Free()

	// Sets.
	set := hash.NewSet[uint8](0, ca)
	for i := 0; i < 1000; i++ {
		set.Add(uint8(i * 7))
	}
	fmt.Printf("set len=%d has(7)=%t add again=%t del=%t has=%t\n", set.Len(), set.Has(7), set.Add(7), set.Delete(7), set.Has(7))
	si := set.Iter()
	total := 0
	for k, ok := si.Next(); ok; k, ok = si.Next() {
		total += int(k)
	}
	fmt.Printf("set total=%d\n", total)
	set.Free()

	// Hash functions.
	fmt.Printf("%x %x\n", hash.FNV1aString(""), hash.FNV1aString("hello"))
	fmt.Printf("%t %t\n", hash.String("abc", 0) == hash.Bytes(store[:0], 0), hash.String("hello world, long string!", 1) == hash.String("hello world, long string!", 1))
	fmt.Printf("live=%d\n", ca.live)

	var zm hash.Map[int, int]
	_, zok := zm.Get(3)
	fmt.Printf("zero map get=%t len=%d\n", zok, zm.Len())
}
