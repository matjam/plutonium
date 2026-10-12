package escape_test

import (
	"strings"
	"testing"
)

func TestCountedErrorBorrows(t *testing.T) {
	const prefix = `package main
import (
 "pluto/alloc"
 "pluto/errors"
)
type E struct { n int }
func (e *E) Error() string { return "error" }
func makeError() error { return alloc.Synced[E]() }
`
	cases := []struct {
		name, body string
		reject     bool
	}{
		{"counted return", `func f() error { e := makeError(); return e }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, false},
		{"borrowed any return", `func f() any { e := makeError(); return any(e) }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, true},
		{"borrowed As return", `func f() *E { e := makeError(); var p *E; errors.As(e, &p); return p }
func main() { _ = f() }`, true},
		{"borrowed helper return", `func borrow(e error) *E { var p *E; errors.As(e, &p); return p }
func f() *E { e := makeError(); return borrow(e) }
func main() { _ = f() }`, true},
		{"borrowed indirect As return", `func f() *E { e := makeError(); var p *E; as := errors.As[*E]; as(e, &p); return p }
func main() { _ = f() }`, true},
		{"counted assertion return", `func f() error { e := makeError(); a := any(e); return a.(error) }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, false},
		{"borrowed caller error", `func f(e error) *E { var p *E; errors.As(e, &p); return p }
func main() { e := makeError(); _ = f(e) }`, false},
		{"counted As target", `func f() error { e := makeError(); var other error; errors.As(e, &other); return other }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, false},
		{"counted helper field", `type holder struct { err error }
func keep(e error, h *holder) { h.err = e }
func f() error { e := makeError(); var h holder; keep(e, &h); return h.err }
func main() { _ = f(); _ = errors.Is(nil, nil) }`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := check(t, prefix+c.body)
			if c.reject {
				if err == nil || !strings.Contains(fullText(err), "E3001") {
					t.Fatalf("want frame-view diagnostic, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}
