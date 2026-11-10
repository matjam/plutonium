package tests

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// Sections 15.4 and 21.5, abi.md 12.10 (finding B4; roadmap item 155): the
// write check for cleaned places runs only once package cleanup has
// started, gated on the program-wide flag rt.cleanupStarted, and the
// unchecked mode keeps it, since it guards owners and counts and not freed
// memory.
func TestCleanupStartedGate(t *testing.T) {
	t.Parallel()
	const finalized = `package main
type item owner struct{id int}
func(p *item)Drop(){if p.id!=0{println("first",p.id)}}
type writer owner struct{live bool}
//pu:nochecks
func poke(){*target=item{}}
func(w *writer)Drop(){if w.live{WRITE;println("unexpected committed")}}
type pair owner struct{first [1]item;last writer}
var target *item
var root=pair{first:[1]item{{id:1}},last:writer{live:true}}
func main(){target=&root.first[0]}
`
	// Before cleanup, writes to the same places, through aliases and of
	// zero values, run as ordinary stores; after main returns the places
	// are cleaned once each.
	const ordinary = `package main
type item owner struct{id int}
func(p *item)Drop(){if p.id>=5000{println("drop",p.id)}}
var root [2]item
var alias *item
func main(){
 alias=&root[1]
 for i:=1;i<=1000;i++{root[0]=item{id:i};*alias=item{id:i};alias.id=0;root[0].id=0}
 root[0]=item{id:5001}
 alias.id=5002
 println("main done",root[0].id,alias.id)
}
`
	const panicked = "first 1\npanic: write to a finalized package resource during shutdown\n"
	for _, tc := range []struct{ name, source, mode, want string }{
		{"checked", strings.Replace(finalized, "WRITE", "*target=item{}", 1), "--gencheck", panicked},
		{"unchecked", strings.Replace(finalized, "WRITE", "*target=item{}", 1), "--gencheck=false", panicked},
		{"noChecksFunction", strings.Replace(finalized, "WRITE", "poke()", 1), "--gencheck", panicked},
		{"noChecksUnchecked", strings.Replace(finalized, "WRITE", "poke()", 1), "--gencheck=false", panicked},
		{"ordinaryChecked", ordinary, "--gencheck", "main done 5001 5002\ndrop 5001\ndrop 5002\n"},
		{"ordinaryUnchecked", ordinary, "--gencheck=false", "main done 5001 5002\ndrop 5001\ndrop 5002\n"},
	} {
		for _, opt := range []int{0, 2} {
			t.Run(tc.name+"/O"+strconv.Itoa(opt), func(t *testing.T) {
				t.Parallel()
				prog := mustBuild(t, single(tc.source), optFlag(opt), tc.mode)
				out, status := runWithTimeout(t, 10*time.Second, prog)
				if tc.want == panicked {
					if status != 2 || !strings.HasPrefix(out, tc.want) || strings.Contains(out, "unexpected committed") {
						t.Fatalf("status %d, output %q; want status 2, the prefix %q and no committed write", status, out, tc.want)
					}
					return
				}
				if status != 0 || out != tc.want {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, tc.want)
				}
			})
		}
	}
}
