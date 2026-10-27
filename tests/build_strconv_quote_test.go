// Ported from internal/build/strconv_quote_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"testing"
)

// Every string form makes one counted buffer; copies outlive their source,
// and every Append form writes bounded storage without allocating. The debug
// allocator independently checks all frees and fails on leaked buffers.
func TestQuoteAllocationLifetimes(t *testing.T) {
	t.Parallel()
	const src = `package main
import (
 "pluto/alloc"
 "pluto/rt"
 "pluto/strconv"
 "pluto/vec"
 "unsafe"
)
type counter struct { a alloc.Allocator; allocations, frees int }
func (c *counter) Alloc(size, align uintptr) unsafe.Pointer {
 c.allocations++
 return c.a.Alloc(size, align)
}
func (c *counter) Free(p unsafe.Pointer, size, align uintptr) {
 c.frees++
 c.a.Free(p, size, align)
}
var counts counter
func value(api int) string {
 switch api {
 case 0: return strconv.Quote("é\u0085")
 case 1: return strconv.QuoteToASCII("é\u0085")
 case 2: return strconv.QuoteToGraphic("\u00a0")
 case 3: return strconv.QuoteRune('\a')
 case 4: return strconv.QuoteRuneToASCII(0x1f600)
 case 5: return strconv.QuoteRuneToGraphic(0x3000)
 }
 panic("bad API")
}
func copies() {
 for api := 0; api < 6; api++ {
  q := value(api)
  held := [2]string{q, q}
  q = ""
  if held[0] != held[1] || len(held[0]) < 3 { panic("lost quote") }
  ascii := api == 1 || api == 3 || api == 4
  if rt.KnownASCII(held[0]) != ascii { panic("wrong ASCII flag") }
 }
}
func appendValue[D vec.Appender[byte]](dst D, api int, s string) {
 switch api {
 case 0: strconv.AppendQuote(dst, s)
 case 1: strconv.AppendQuoteToASCII(dst, s)
 case 2: strconv.AppendQuoteToGraphic(dst, s)
 case 3: strconv.AppendQuoteRune(dst, '\a')
 case 4: strconv.AppendQuoteRuneToASCII(dst, -1)
 case 5: strconv.AppendQuoteRuneToGraphic(dst, 0x3000)
 }
}
func bounded() {
 var source [600]byte
 for i := range source { source[i] = 'a' }
 source[63], source[64] = 0xc3, 0xa9
 source[254], source[255], source[256], source[257] = 0xf0, 0x9f, 0x98, 0x80
 source[511] = 0xff
 s := unsafe.String(&source[0], len(source))
 var mem [48]byte
 for capacity := 0; capacity <= len(mem); capacity++ {
  for api := 0; api < 6; api++ {
   f := vec.NewFixed(mem[:capacity])
   appendValue(&f, api, s)
   if capacity == 0 && !f.Truncated() { panic("missing truncation") }
  }
 }
}
func main() {
 counts.a = alloc.Default()
 alloc.SetDefault(&counts)
 copies()
 println("strings", counts.allocations, counts.frees)
 bounded()
 println("append", counts.allocations, counts.frees)
}
`
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			t.Parallel()
			got, status := runDebugAlloc(t, buildDebugAlloc(t, src, level), "")
			const want = "strings 6 6\nappend 6 6\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}
