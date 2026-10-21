package solve

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape2/eir"
)

// A solve past its limits stops with an error naming the function, never
// with a summary (which would accept what it did not finish checking).
func TestLimitError(t *testing.T) {
	for _, lower := range []func(){
		func() { maxFuncSteps = 3 },
		func() { maxFuncArena = 1 },
	} {
		saved := [2]int{maxFuncArena, maxFuncSteps}
		lower()
		e := newEnv()
		b := e.fn("big", 1, 1)
		x := b.obj("x", eir.LifeFrame, -1)
		v := b.input(0)
		for range 20 {
			v = b.load(b.field(v, 1))
		}
		b.store(b.addr(x), v)
		b.ret(0, b.load(b.addr(x)))
		func() {
			defer func() {
				maxFuncArena, maxFuncSteps = saved[0], saved[1]
				le, ok := recover().(*LimitError)
				if !ok {
					t.Fatalf("no LimitError")
				}
				if le.Func != "big" || !strings.Contains(le.Error(), "the escape analysis of big reached its") {
					t.Errorf("error: %s", le.Error())
				}
			}()
			e.solve(t, b.f)
		}()
	}
}
