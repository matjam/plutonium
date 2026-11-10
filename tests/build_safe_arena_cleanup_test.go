// Ported from internal/build/safe_arena_cleanup_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"testing"
)

// Sections 17.3 and 18.6 require an exiting allocator callback to finish the
// owner's remaining required record/backing cleanup before propagating exit:
// every request the arena made of its parent is given back.
func TestSafeArenaDropCallback(t *testing.T) {
	t.Parallel()
	for _, constructor := range []string{"arena.NewFixed(256,c)", "arena.NewChunked(256,c)"} {
		for _, exiting := range []bool{false, true} {
			for _, opt := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/exit%t/O%d", constructor, exiting, opt), func(t *testing.T) {
					t.Parallel()
					source := `package main
import ("pluto/alloc";"pluto/arena";"pluto/rt";"unsafe")
type counter struct {base alloc.Allocator; live,allocations,frees,after int; fail bool}
func(c *counter)Alloc(size,align uintptr)unsafe.Pointer{
 p:=c.base.Alloc(size,align);if p!=nil{c.live++;c.allocations++};return p
}
func(c *counter)Free(p unsafe.Pointer,size,align uintptr){
 c.base.Free(p,size,align);if p!=nil{c.live--;c.frees++}
 if c.fail{rt.ExitTask()};c.after++
}
func exercise(c *counter){a:=` + constructor + `
 _=a.New[int]();_=a.New[int]();c.fail=` + fmt.Sprint(exiting) + `
}
func main(){c:=counter{base:alloc.Default()}
 exited:=rt.CatchExitTask(func(){exercise(&c)})
 println(exited,c.live,c.allocations,c.frees,c.after)
}
`
					// A Fixed asks its parent once, for its payload and its
					// bookkeeping together (abi.md section 1.3, D54); a
					// Chunked makes three requests here.
					n := 3
					if constructor == "arena.NewFixed(256,c)" {
						n = 1
					}
					want := fmt.Sprintf("false 0 %d %d %d\n", n, n, n)
					if exiting {
						want = fmt.Sprintf("true 0 %d %d 0\n", n, n)
					}
					out, status := runSrc(t, source, opt, "--gencheck")
					if status != 0 || out != want {
						t.Fatalf("status %d, output %q; want complete cleanup %q", status, out, want)
					}
				})
			}
		}
	}
}
