// Ported from internal/build/hash_stream_contract_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"testing"
)

func TestSipHashUninitialized(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"Write(nil)", "WriteString(\"\")", "WriteByte(0)", "Reset()", "Seed()", "Sum64()"} {
		for _, level := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", method, level), func(t *testing.T) {
				t.Parallel()
				src := `package main
import ( "pluto/hash"; "pluto/rt"; "pluto/os" )
//pu:panic
func fail(p *rt.Panic) { println(p.Message); os.Exit(2) }
func main() { var h hash.SipHash24; h.` + method + ` }
`
				got, status := runSrc(t, src, level)
				if status != 2 || got != "hash: SipHash24 requires a key\n" {
					t.Fatalf("exit %d, output %q", status, got)
				}
			})
		}
	}
}

func TestHashStreamingNoAllocations(t *testing.T) {
	t.Parallel()
	const src = `package main
import ( "pluto/hash"; "pluto/alloc"; "pluto/alloc/debug"; "pluto/io" )
var data [1031]byte
func write(w io.Writer, b []byte) { n, e := w.Write(b); if n != len(b) || e != nil { panic("writer") } }
func exercise() {
 var a hash.FNV32a
 var z hash.FNV64a
 var key [16]byte
 for i := 0; i < 16; i++ { key[i] = byte(i) }
 h := hash.NewSipHash24(key)
 for n := 0; n < 300; n++ {
  write(&a, data[:]); write(&z, data[:]); write(&h, data[:])
  a.WriteString("hello, 世界"); z.WriteString("hello, 世界"); h.WriteString("hello, 世界")
  a.WriteByte(byte(n)); z.WriteByte(byte(n)); h.WriteByte(byte(n))
  c := h
  c.WriteByte(255)
  if c.Sum64() == h.Sum64() { panic("copy") }
  _ = a.Sum32(); _ = z.Sum64(); _ = h.Sum64()
  h.SetSeed(h.Seed())
  h.Reset(); a.Reset(); z.Reset()
 }
}
func main() {
 d := alloc.Default().(*debug.Allocator)
 before := d.Stats()
 exercise()
 after := d.Stats()
 println(after.Allocs-before.Allocs, after.Live-before.Live, after.LiveBytes-before.LiveBytes)
}
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			t.Parallel()
			exe := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, exe, "")
			if status != 0 || got != "0 0 0\n" {
				t.Fatalf("exit %d, output %q", status, got)
			}
		})
	}
}
