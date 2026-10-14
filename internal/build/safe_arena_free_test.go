package build_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

func TestSafeArenaFree(t *testing.T) {
	testguard.Parallel(t)
	for _, constructor := range []string{"arena.NewFixed(4096)", "arena.NewChunked()"} {
		for _, stale := range []bool{false, true} {
			for _, opt := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/stale%t/O%d", constructor, stale, opt), func(t *testing.T) {
					testguard.Parallel(t)
					body := `p:=a.New[int](); *p=7
 q:=a.New[int](); *q=8
 a.Free(unsafe.Pointer(q),8,8)
 r:=a.New[int](); *r=9
 println(*p,*r)`
					if stale {
						body = `p:=a.New[int](); *p=7
 a.Reset()
 q:=a.New[int](); *q=9
 a.Free(unsafe.Pointer(p),8,8)
 println("accepted stale Free",*q)`
					}
					source := "package main\nimport (\"pluto/arena\";\"unsafe\")\nfunc main(){a:=" + constructor + "\n" + body + "\n}\n"
					out, status := runGenCheck(t, source, opt)
					if stale {
						if status != 2 || !strings.Contains(out, "use of memory from an arena") || strings.Contains(out, "accepted stale Free") {
							t.Fatalf("status %d, output %q; want stale Free rejected before current allocation is reclaimed", status, out)
						}
					} else if status != 0 || out != "7 9\n" {
						t.Fatalf("status %d, output %q; want live prefix/latest-free reuse output 7 9", status, out)
					}
				})
			}
		}
	}
}
