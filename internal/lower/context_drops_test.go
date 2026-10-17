package lower_test

import "testing"

// TestInterruptContextDrops checks that the wait check follows the drops
// the compiler inserts (language spec section 20.10): at the end of a
// scope, on assignment over an owner, for a temporary, when a counted
// value's count reaches zero, and into the Free of an installed allocator.
func TestInterruptContextDrops(t *testing.T) {
	const decls = "type Conn owner struct{ id int }\n\n" +
		"func (c *Conn) Drop() { wait() }\n\n" +
		"func (c *Conn) Touch() int { return c.id }\n\n" +
		"func mk() Conn { return Conn{id: 1} }\n\n" +
		"func use(c *Conn) {}\n\n" +
		"type slowFree struct{}\n\n" +
		"func (slowFree) Alloc(size, align uintptr) unsafe.Pointer { return nil }\n\n" +
		"func (slowFree) Free(p unsafe.Pointer, size, align uintptr) { wait() }\n\n" +
		"type Plain owner struct{ n int }\n\n" +
		"type Pair owner struct{ a, b Conn }\n\n" +
		"func useP(p *Pair) {}\n\n" +
		"func eat(c Conn) {}\n\n" +
		"var name string\n\n" +
		"func outb()\n\n" +
		"var flag bool\n\n//pu:blocking\nfunc spinUntilSet() {\n\tfor !flag {\n\t}\n}\n\n" +
		"var setupFn = setup\n\nfunc setup() { alloc.SetDefault(slowFree{}) }\n\n"
	const irq = "keyboard is an interrupt handler, so it must not wait"
	cases := []struct{ name, body, want string }{
		{"end of scope", "\tc := mk()\n\tuse(&c)\n", irq},
		{"assignment over an owner", "\tc := mk()\n\tuse(&c)\n\tc = mk()\n\tuse(&c)\n\tx86.Hlt()\n", irq},
		{"temporary", "\t_ = mk().Touch()\n", irq},
		{"counted release", "\ts := alloc.Shared[Conn]()\n\tuse(&*s)\n", irq},
		{"free of the allocator", "\tp := alloc.Owned[Plain]()\n\t_ = p\n", irq},
		{"fields of an owner", "\tp := Pair{a: mk()}\n\tuseP(&p)\n", irq},
		{"array of owners", "\ta := [2]Conn{mk(), mk()}\n\tuse(&a[0])\n", irq},
		{"discarded result", "\tmk()\n", irq},
		{"parameter of a call", "\teat(mk())\n", irq},
		{"clear of a slice", "\tvar a [2]Conn\n\tuse(&a[0])\n\tclear(a[:])\n", irq},
		{"counted string", "\ts := name + \"!\"\n\t_ = len(s)\n", irq},
		{"early drop", "\tc := mk()\n\tuse(&c)\n\tc.Drop()\n", irq},
		// A function without a body is taken at its declaration: it does not
		// wait unless the declaration says so (wait is marked).
		{"bodyless with the directive", "\twait()\n", irq},
		{"bodyless without the directive", "\toutb()\n", ""},
		{"spin with a body, marked", "\tspinUntilSet()\n", irq},
		{"drop that does not wait", "\tvar c Plain\n\t_ = c.n\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "package main\n\nimport (\n\t\"pluto/alloc\"\n\t\"pluto/arch/x86\"\n\t\"pluto/rt\"\n\t\"unsafe\"\n)\n\n" +
				"var _ alloc.Allocator\nvar _ unsafe.Pointer\n\n" + goodHandler + contextDecls + decls +
				"//pu:interrupt\nfunc keyboard(f *x86.InterruptFrame) {\n" + c.body + "}\n"
			got := lowerModeError(t, src, kernel)
			if c.want == "" && got != "" {
				t.Fatalf("unexpected error:\n%s", got)
			}
			if c.want != "" && !containsText(got, c.want) {
				t.Fatalf("error does not contain %q:\n%s", c.want, got)
			}
		})
	}
}
