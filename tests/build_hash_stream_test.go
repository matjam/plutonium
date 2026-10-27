// Ported from internal/build/hash_stream_test.go at the go-cutover tag.

package tests

import (
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"testing"
)

// The generated corpus uses hex so string writes can cover arbitrary bytes
// through an explicit unsafe view without UTF-8 conversion or slicing.
const hashStreamHarness = `package main
import (
 "pluto/hash"
 "unsafe"
)
var buf [4096]byte
func unhex(s string) []byte {
 const digits = "0123456789abcdef"
 for i := 0; i < len(s)/2; i++ {
  var a, b byte
  for j := 0; j < 16; j++ {
   if s[2*i] == digits[j] { a = byte(j) }
   if s[2*i+1] == digits[j] { b = byte(j) }
  }
  buf[i] = a<<4 | b
 }
 return buf[:len(s)/2]
}
func view(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }
func check(text string, key [16]byte, w32 uint32, w64, ws, replaced uint64) {
 b := unhex(text)
 var a hash.FNV32a
 var z hash.FNV64a
 h := hash.NewSipHash24(key)
 if a.Sum32() != 2166136261 || z.Sum64() != 14695981039346656037 { panic("FNV zero value") }
 a.Write(b); z.Write(b); h.Write(b)
 if a.Sum32() != w32 || z.Sum64() != w64 || h.Sum64() != ws { panic("whole byte write") }
 if a.Sum32() != w32 || z.Sum64() != w64 || h.Sum64() != ws { panic("repeated sum") }
 // Every split for small messages, representative splits for larger messages.
 for cut := 0; cut <= len(b); cut++ {
  if len(b) > 257 && cut > 16 && cut < len(b)-16 && cut%127 != 0 { continue }
  a.Reset(); z.Reset(); h.Reset()
  n, e := a.Write(b[:cut]); if n != cut || e != nil { panic("FNV32 write contract") }
  n, e = z.Write(b[:cut]); if n != cut || e != nil { panic("FNV64 write contract") }
  n, e = h.Write(b[:cut]); if n != cut || e != nil { panic("Sip write contract") }
  ac, zc, hc := a, z, h
  _ = a.Sum32(); _ = z.Sum64(); _ = h.Sum64()
  n, e = a.WriteString(view(b[cut:])); if n != len(b)-cut || e != nil { panic("FNV32 string contract") }
  n, e = z.WriteString(view(b[cut:])); if n != len(b)-cut || e != nil { panic("FNV64 string contract") }
  n, e = h.WriteString(view(b[cut:])); if n != len(b)-cut || e != nil { panic("Sip string contract") }
  for _, c := range b[cut:] {
   if ac.WriteByte(c) != nil || zc.WriteByte(c) != nil || hc.WriteByte(c) != nil { panic("byte contract") }
  }
  if a.Sum32() != w32 || z.Sum64() != w64 || h.Sum64() != ws { panic("partition/continued write") }
  if ac.Sum32() != w32 || zc.Sum64() != w64 || hc.Sum64() != ws { panic("copied state") }
  a.Write(nil); z.Write(nil); h.Write(nil)
  a.WriteString(""); z.WriteString(""); h.WriteString("")
  if a.Sum32() != w32 || z.Sum64() != w64 || h.Sum64() != ws { panic("empty writes") }
 }
 // SetSeed resets and takes a value copy. Seed returns a value copy.
 var fresh hash.SipHash24
 fresh.SetSeed(key)
 saved := fresh.Seed()
 key[0] ^= 255
 saved[1] ^= 255
 fresh.Write(b)
 if fresh.Sum64() != ws { panic("key copy/reset") }
 h.SetSeed(fresh.Seed()); h.WriteString(view(b))
 if h.Sum64() != ws { panic("key replacement") }
 key = fresh.Seed()
 for i := 0; i < 16; i++ { key[i] ^= 255 }
 h.SetSeed(key); h.Write(b)
 if h.Sum64() != replaced { panic("different key replacement") }
 // Alternate all three writers across successive block and tail boundaries.
 a.Reset(); z.Reset(); h.SetSeed(fresh.Seed())
 for i := 0; i < len(b); {
  end := i+1+i%17
  if end > len(b) { end = len(b) }
  switch i%3 {
  case 0: a.Write(b[i:end]); z.Write(b[i:end]); h.Write(b[i:end])
  case 1: a.WriteString(view(b[i:end])); z.WriteString(view(b[i:end])); h.WriteString(view(b[i:end]))
  case 2:
   for _, c := range b[i:end] { a.WriteByte(c); z.WriteByte(c); h.WriteByte(c) }
  }
  _ = a.Sum32(); _ = z.Sum64(); _ = h.Sum64()
  i = end
 }
 if a.Sum32() != w32 || z.Sum64() != w64 || h.Sum64() != ws { panic("mixed writers") }
}
func main() {
`

func fnvOracle(b []byte) (uint32, uint64) {
	a, z := fnv.New32a(), fnv.New64a()
	_, _ = a.Write(b)
	_, _ = z.Write(b)
	return a.Sum32(), z.Sum64()
}

func TestHashStreamingDifferential(t *testing.T) {
	t.Parallel()
	// Validate the independent one-shot oracle before relying on it here.
	var official [16]byte
	var bytes256 [256]byte
	for i := range official {
		official[i] = byte(i)
	}
	for i := range bytes256 {
		bytes256[i] = byte(i)
	}
	for n, want := range sipHashVectors {
		if sipHashOracle(official, bytes256[:n]) != want {
			t.Fatalf("oracle vector %d", n)
		}
	}
	corpus := [][]byte{nil, []byte("hello, 世界"), {0xff, 0xc0, 0x80, 0, 0xfe}, bytes256[:], {204, 36, 49, 196}, {204, 36, 49, 196, 42}}
	if sum, _ := fnvOracle(corpus[4]); sum != 0 {
		t.Fatal("zero-state corpus oracle")
	}
	for n := range 65 {
		corpus = append(corpus, bytes256[:n])
	}
	rng := rand.New(rand.NewPCG(0x51f24, 0xf32a64a))
	for _, n := range []int{127, 128, 129, 254, 255, 256, 257, 258, 511, 512, 513, 1023, 1024, 1025, 4096} {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rng.Uint32())
		}
		corpus = append(corpus, b)
	}
	var source strings.Builder
	source.WriteString(hashStreamHarness)
	keys := [][16]byte{official, {}, {255, 254, 253, 252, 251, 250, 249, 248, 247, 246, 245, 244, 243, 242, 241, 240}}
	for range 4 {
		var key [16]byte
		for i := range key {
			key[i] = byte(rng.Uint32())
		}
		keys = append(keys, key)
	}
	for _, key := range keys {
		other := key
		for i := range other {
			other[i] ^= 255
		}
		for _, msg := range corpus {
			a, z := fnvOracle(msg)
			fmt.Fprintf(&source, "check(%q, [16]byte{%s}, %d, %d, %d, %d)\n", hex.EncodeToString(msg), hashKeyLiteral(key), a, z, sipHashOracle(key, msg), sipHashOracle(other, msg))
		}
	}
	source.WriteString("println(\"streaming hash oracle passed\")\n}\n")
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			t.Parallel()
			exe := buildDebugAlloc(t, source.String(), level)
			got, status := runDebugAlloc(t, exe, "")
			if status != 0 || got != "streaming hash oracle passed\n" {
				t.Fatalf("exit %d, output %s", status, got)
			}
		})
	}
}

func hashKeyLiteral(key [16]byte) string {
	var parts [16]string
	for i, b := range key {
		parts[i] = fmt.Sprint(b)
	}
	return strings.Join(parts[:], ",")
}
