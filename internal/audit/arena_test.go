package audit

import (
	"strings"
	"testing"
)

// The manual API's actual declaration determines its unsafe label. Ordinary
// Fixed methods and unrelated same-name methods do not inherit that label.
func TestManualArenaOperations(t *testing.T) {
	t.Parallel()
	out := report(t, `package sample
import "pluto/arena"
func construct(buf []byte) arena.Arena { return arena.NewUnsafe(buf) }
func typed(a *arena.Arena) { _=a.New[int](); _=a.Slice[int](1) }
func reclaim(a *arena.Arena) { m:=a.Mark(); a.Release(m); a.Reset() }
func saved(a *arena.Arena) { f:=a.Reset; f() }
func factory(buf []byte) arena.Arena { f:=arena.NewUnsafe; return f(buf) }
func fixed() arena.Fixed { return arena.NewFixed(64) }
func ordinary(a *arena.Fixed) { a.Reset(); _=a.New[int]() }
type Fake struct{}
func (*Fake) Reset() {}
func unrelated(f *Fake) { f.Reset() }
func NewUnsafe(buf []byte) int { return len(buf) }
func lookalike(buf []byte) { _=NewUnsafe(buf) }
func query(a *arena.Arena) { _=a.Used(); _=a.Mark() }
`)
	golden(t, "arena_operations", out)
	for _, reason := range []string{"pluto/arena.NewUnsafe", "manual Arena.New", "manual Arena.Slice", "manual Arena.Reset", "manual Arena.Release"} {
		if !strings.Contains(out, reason) {
			t.Errorf("missing %q:\n%s", reason, out)
		}
	}
	for _, name := range []string{"construct", "typed", "reclaim", "saved", "factory"} {
		if !strings.Contains(out, "unsafe main."+name+" ") {
			t.Errorf("missing direct/manual operation in %s:\n%s", name, out)
		}
	}
	for _, name := range []string{"fixed", "ordinary", "unrelated", "lookalike", "query"} {
		if strings.Contains(out, "unsafe main."+name+" ") {
			t.Errorf("ordinary/unrelated operation labeled unsafe in %s:\n%s", name, out)
		}
	}
}
