package build_test

import (
	"fmt"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Sections 17.3 and 18.6 require an exiting allocator callback to finish the
// owner's remaining required record/backing cleanup before propagating exit.
func TestSafeArenaDropCallback(t *testing.T) {
	testguard.Parallel(t)
	for _, constructor := range []string{"arena.NewFixed(256,c)", "arena.NewChunked(256,c)"} {
		for _, exiting := range []bool{false, true} {
			for _, opt := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/exit%t/O%d", constructor, exiting, opt), func(t *testing.T) {
					testguard.Parallel(t)
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
					want := "false 0 3 3 3\n"
					if exiting {
						want = "true 0 3 3 0\n"
					}
					out, status := runGenCheck(t, source, opt)
					if status != 0 || out != want {
						t.Fatalf("status %d, output %q; want complete cleanup %q", status, out, want)
					}
				})
			}
		}
	}
}
