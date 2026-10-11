package escape_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Scoped tasks (scope.go): a task started by a Scope of the function may
// use the frame, but what it takes must not look at storage a loop reuses
// before the Scope waits.
func TestScopeViews(t *testing.T) {
	testguard.Parallel(t)
	const head = "package main\n\nimport \"pluto/sync\"\n\nfunc send(b []byte) { println(len(b)) }\n"
	rejected := []struct{ name, src, want string }{
		{"view of the loop body's array", `
func main() {
	var s sync.Scope
	for _, line := range []string{"a"} {
		var buf [8]byte
		v := buf[:copy(buf[:], line)]
		s.Task(func() { send(v) })
	}
}`, "main.p:12:3: the task started here takes v, a view of buf, which the loop at main.p:9 reuses before s waits for the task"},
		{"pointer to a loop variable", `
func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		p := &i
		s.Task(func() { println(*p) })
	}
}`, "the task started here takes p, a view of i"},
		{"plain go keeps its rule", `
func use(p *int) { println(*p) }

func main() {
	n := 1
	p := &n
	task use(p)
	var s sync.Scope
	s.Wait()
}`, "Or start it with a sync.Scope, which waits for its tasks before its block ends, so that they may use main's variables: var s sync.Scope s.Task(func() { use(p) })"},
	}
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := check(t, head+c.src+"\n")
			if err == nil {
				t.Fatal("no error reported")
			}
			flat := strings.Join(strings.Fields(fullText(err)), " ")
			if !strings.Contains(flat, c.want) {
				t.Errorf("error does not contain %q:\n%s", c.want, fullText(err))
			}
		})
	}
	accepted := []struct{ name, src string }{
		{"views of the frame", `
func main() {
	var buf [8]byte
	n := 3
	var s sync.Scope
	s.Task(func() { send(buf[:n]) })
	p := &n
	s.Task(func() { println(*p) })
}`},
		{"the loop body's array itself", `
func main() {
	var s sync.Scope
	for _, line := range []string{"a"} {
		var buf [8]byte
		n := copy(buf[:], line)
		s.Task(func() { send(buf[:n]) })
	}
}`},
		{"a scope that waits in the loop", `
func main() {
	for _, line := range []string{"a"} {
		var s sync.Scope
		var buf [8]byte
		v := buf[:copy(buf[:], line)]
		s.Task(func() { send(v) })
	}
}`},
	}
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := check(t, head+c.src+"\n"); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}
