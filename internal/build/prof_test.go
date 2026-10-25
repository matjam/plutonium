package build_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// profSrc spins in main.spin, called from main and from a task, under the
// CPU profiler, then allocates in main.small and main.big under the
// allocation profiler: 80 MB in 800-byte vectors and 240 MB in 2.4 MB ones.
const profSrc = `package main

import (
	"pluto/os"
	"pluto/prof"
	"pluto/vec"
)

//pu:noinline
func spin(n int) int {
	x := 0
	for i := 0; i < n; i++ {
		x = x*31 + i ^ (x >> 7)
	}
	return x
}

//pu:noinline
func outer(n int) int { return spin(n) }

func worker(n int, done chan int) {
	done <- outer(n)
}

//pu:noinline
func small() int {
	v := vec.Make[int](100)
	n := v.Len()
	v.Free()
	return n
}

//pu:noinline
func big() int {
	v := vec.Make[int](300000)
	n := v.Len()
	v.Free()
	return n
}

func main() {
	dir := os.GetenvLossy("OUT")
	if err := prof.StartCPUProfile(); err != nil {
		println(err.Error())
		os.Exit(1)
	}
	done := make(chan int)
	task worker(300000000, done)
	t := outer(300000000)
	t += <-done
	if err := prof.StopCPUProfile(dir + "/cpu.prof"); err != nil {
		println(err.Error())
		os.Exit(1)
	}
	if err := prof.StartAllocProfile(0); err != nil {
		println(err.Error())
		os.Exit(1)
	}
	for k := 0; k < 100000; k++ {
		t += small()
	}
	for k := 0; k < 100; k++ {
		t += big()
	}
	if err := prof.StopAllocProfile(dir + "/mem.prof"); err != nil {
		println(err.Error())
		os.Exit(1)
	}
	println(t != 0)
}
`

// TestProfiles checks pluto/prof's CPU and allocation profiles: they
// decode as pprof profiles, the CPU time is in main.spin with its callers
// (on the main stack and on a task's), and the allocations are charged to
// the code that asked for the memory, in proportion.
func TestProfiles(t *testing.T) {
	testguard.Parallel(t)
	for _, level := range []int{0, 2} {
		prog := buildConc(t, profSrc, build.Config{OptLevel: level, NoGenCheck: true})
		dir := t.TempDir()
		out, status := runConc(t, prog, "OUT="+dir)
		if status != 0 || out != "true\n" {
			t.Fatalf("-O%d: status %d, output %q", level, status, out)
		}

		cpu := readProfile(t, filepath.Join(dir, "cpu.prof"))
		if got := cpu.typ(); got != "samples/count cpu/nanoseconds" {
			t.Errorf("-O%d: cpu sample types %q", level, got)
		}
		var spin, total int64
		viaTask := false
		for _, s := range cpu.samples {
			total += s.values[0]
			if len(s.stack) > 0 && s.stack[0] == "main.spin" {
				spin += s.values[0]
				if strings.HasPrefix(strings.Join(s.stack, " ")+" ", "main.spin main.outer main.worker ") {
					viaTask = true
				}
			}
		}
		if total < 20 || spin*10 < total*8 {
			t.Errorf("-O%d: %d of %d cpu samples in main.spin\n%s", level, spin, total, cpu.dump())
		}
		if !viaTask {
			t.Errorf("-O%d: no cpu sample in main.spin under the task\n%s", level, cpu.dump())
		}

		mem := readProfile(t, filepath.Join(dir, "mem.prof"))
		if got := mem.typ(); got != "alloc_objects/count alloc_space/bytes" {
			t.Errorf("-O%d: alloc sample types %q", level, got)
		}
		var small, big, all int64
		for _, s := range mem.samples {
			all += s.values[1]
			if len(s.stack) > 0 && strings.HasPrefix(s.stack[0], "pluto/prof.") {
				t.Errorf("-O%d: allocation charged to the profiler: %v", level, s.stack)
			}
			for _, f := range s.stack {
				switch f {
				case "main.small":
					small += s.values[1]
				case "main.big":
					big += s.values[1]
				}
			}
		}
		// 80 MB and 240 MB, sampled every 512 KiB.
		if small < 60<<20 || small > 100<<20 || big < 220<<20 || big > 260<<20 {
			t.Errorf("-O%d: small %d bytes, big %d, of %d\n%s", level, small, big, all, mem.dump())
		}
	}
}

// A decoded profile: its sample types, and its samples' stacks as
// function names, innermost first, with inlined calls as frames.
type decodedProfile struct {
	types   []string
	samples []decodedSample
}

type decodedSample struct {
	stack  []string
	values []int64
}

func (p *decodedProfile) typ() string { return strings.Join(p.types, " ") }

func (p *decodedProfile) dump() string {
	var b strings.Builder
	for _, s := range p.samples {
		b.WriteString(strings.Join(s.stack, " < "))
		for _, v := range s.values {
			b.WriteString(" ")
			b.WriteString(itoa(v))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func itoa(v int64) string {
	var buf [24]byte
	i := len(buf)
	neg := v < 0
	if neg {
		v = -v
	}
	for {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// protoFields splits a protobuf message into its fields, calling f with
// each field's number and wire type, and its value (varint) or bytes.
func protoFields(t *testing.T, b []byte, f func(num int, v uint64, data []byte)) {
	t.Helper()
	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			t.Fatal("profile: bad field key")
		}
		b = b[n:]
		num, wt := int(key>>3), key&7
		switch wt {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				t.Fatal("profile: bad varint")
			}
			b = b[n:]
			f(num, v, nil)
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				t.Fatal("profile: bad length")
			}
			f(num, 0, b[n:n+int(l)])
			b = b[n+int(l):]
		default:
			t.Fatalf("profile: unexpected wire type %d", wt)
		}
	}
}

// packed returns the values of a repeated varint field, packed or not.
func packed(v uint64, data []byte) []uint64 {
	if data == nil {
		return []uint64{v}
	}
	var out []uint64
	for len(data) > 0 {
		x, n := binary.Uvarint(data)
		if n <= 0 {
			break
		}
		out = append(out, x)
		data = data[n:]
	}
	return out
}

// readProfile decodes the uncompressed profile.proto in name.
func readProfile(t *testing.T, name string) *decodedProfile {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var strs []string
	type valueType struct{ typ, unit uint64 }
	var types []valueType
	type rawSample struct {
		locs   []uint64
		values []uint64
	}
	var samples []rawSample
	locs := map[uint64][]uint64{} // location id -> function ids, innermost first
	funcs := map[uint64]uint64{}  // function id -> name string index
	protoFields(t, b, func(num int, _ uint64, data []byte) {
		switch num {
		case 1:
			var vt valueType
			protoFields(t, data, func(num int, v uint64, _ []byte) {
				switch num {
				case 1:
					vt.typ = v
				case 2:
					vt.unit = v
				}
			})
			types = append(types, vt)
		case 2:
			var s rawSample
			protoFields(t, data, func(num int, v uint64, d []byte) {
				switch num {
				case 1:
					s.locs = append(s.locs, packed(v, d)...)
				case 2:
					s.values = append(s.values, packed(v, d)...)
				}
			})
			samples = append(samples, s)
		case 4:
			var id uint64
			var fns []uint64
			protoFields(t, data, func(num int, v uint64, d []byte) {
				switch num {
				case 1:
					id = v
				case 4:
					protoFields(t, d, func(num int, v uint64, _ []byte) {
						if num == 1 {
							fns = append(fns, v)
						}
					})
				}
			})
			locs[id] = fns
		case 5:
			var id, name uint64
			protoFields(t, data, func(num int, v uint64, _ []byte) {
				switch num {
				case 1:
					id = v
				case 2:
					name = v
				}
			})
			funcs[id] = name
		case 6:
			strs = append(strs, string(data))
		}
	})
	str := func(i uint64) string {
		if i >= uint64(len(strs)) {
			t.Fatalf("profile: string %d of %d", i, len(strs))
		}
		return strs[i]
	}
	if len(strs) == 0 || strs[0] != "" {
		t.Fatal("profile: string table does not start with \"\"")
	}
	p := &decodedProfile{}
	for _, vt := range types {
		p.types = append(p.types, str(vt.typ)+"/"+str(vt.unit))
	}
	for _, s := range samples {
		var ds decodedSample
		for _, l := range s.locs {
			fns, ok := locs[l]
			if !ok {
				t.Fatalf("profile: sample names location %d, which is missing", l)
			}
			for _, fn := range fns {
				name, ok := funcs[fn]
				if !ok {
					t.Fatalf("profile: location names function %d, which is missing", fn)
				}
				ds.stack = append(ds.stack, str(name))
			}
		}
		for _, v := range s.values {
			ds.values = append(ds.values, int64(v))
		}
		if len(ds.values) != len(p.types) {
			t.Fatalf("profile: sample has %d values for %d types", len(ds.values), len(p.types))
		}
		p.samples = append(p.samples, ds)
	}
	return p
}
