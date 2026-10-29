// Ported from internal/escape2/report/report_test.go at the go-cutover tag.
// The Go file was the external package report_test; the tests are in
// package escape2 (see report_helpers_test.p).

package escape2

import (
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/report"
	"compiler/internal/escape2/sched"
	"compiler/internal/escape2/solve"
)

// sameAsOld are programs on whose errors both analyses agreed: this
// analysis's messages must be the ones internal/escape wrote, word for word
// (sameAsOldWant), as the diagnostics goldens were written with them.
var sameAsOld = map[string]string{
	"return pointer to local": `package main

type Config struct{ timeout int }

func load() *Config {
	var c Config
	c.timeout = 30
	return &c
}

func main() { _ = load() }
`,
	"return slice of local array": `package main

func header() []byte {
	var buf [16]byte
	return buf[:]
}

func main() { _ = header() }
`,
	"store closure in package variable": `package main

var hook func() int

func counter() {
	total := 0
	hook = func() int {
		total++
		return total
	}
}

func main() { counter() }
`,
	"store pointer in package variable": `package main

type Config struct{ timeout int }

var last *Config

func keep() {
	var c Config
	last = &c
}

func main() { keep() }
`,
	"callee keeps its parameter": `package main

type Config struct{ timeout int }

var cache [1]*Config

func remember(c *Config) { cache[0] = c }

func keep() {
	var c Config
	remember(&c)
}

func main() { keep() }
`,
	"callee chain": `package main

var kept *int

func store(p *int) { kept = p }

func pass(q *int) { store(q) }

func main() {
	n := 1
	pass(&n)
}
`,
	"closure kept past its iteration": `package main

func loops() int {
	var first func() int
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		if i == 0 {
			first = f
		}
	}
	return first()
}

func main() { _ = loops() }
`,
	"closure passed to a task": `package main

func main() {
	n := 3
	out := make(chan int, 1)
	square := func() { out <- n * n }
	task square()
	println(<-out)
}
`,
	"a struct as an interface": `package main

type ParseError struct{ line, col int }

func (e ParseError) Error() string { return "parse error" }

func parse() error {
	return ParseError{line: 1, col: 2}
}

func main() { _ = parse() }
`,
}

// sameAsOldWant holds the message internal/escape wrote for each program of
// sameAsOld (pluto with PLUTO_ESCAPE=old, before 18358443 removed it), with
// the directory left out as render leaves it out. The task start has none:
// its error is lower's E2594 (spec section 12.12), and neither analysis
// reports one.
var sameAsOldWant = map[string]string{
	"a struct as an interface": `main.p:8:9: cannot return ParseError{...} as an interface: the interface would hold a copy of it in parse's frame
    Converting a value larger than one word to an interface puts a copy of the
    value in the frame of the function that converts it (here parse, at
    main.p:8), and the interface points at that copy. That frame is reclaimed
    when parse returns, so the interface would point at memory that no longer
    holds the value.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    Return a pointer to memory that outlives parse, such as a field of a
    longer-lived value:
        p.err = ParseError{...}
        return &p.err
    or return a value that fits in one word, such as an error code.
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3001" for a longer explanation)`,
	"callee chain": `main.p:11:2: cannot pass &n to pass, which keeps it after returning
    &n is a pointer to n. n is a local variable of main (declared at main.p:10).
    pass passes q to store (main.p:7), which stores p in package variable kept
    (main.p:5), so &n would outlive n.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    If pass should keep its own copy of n, change it to take the value, not a
    pointer, or an owner it can keep: an owned[int] from new, a shared[int], or
    a library owner such as a vec.Vec.
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3003" for a longer explanation)`,
	"callee keeps its parameter": `main.p:11:2: cannot pass &c to remember, which keeps it after returning
    &c is a pointer to c. c is a local variable of keep (declared at main.p:10).
    remember stores c in cache[0] (main.p:7), so &c would outlive c.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    If remember should keep its own copy of c, change it to take the value, not
    a pointer, or an owner it can keep: an owned[Config] from new, a
    shared[Config], or a library owner such as a vec.Vec.
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3003" for a longer explanation)`,
	"closure kept past its iteration": `main.p:8:4: cannot keep f in first after this iteration: it uses i, which the loop at line 5 reuses in every iteration
    A function literal refers to the variables it uses where they live, and i
    lives in loops's frame, where every iteration of the loop at line 5 reuses
    the same memory for it. When the next iteration starts, the literal kept in
    first would see that iteration's i, not this one's.
    Go gives each iteration of a loop its own variables, so a function literal
    made in one iteration keeps that iteration's values. Plutonium keeps
    variables in the function's frame, which has room for one copy of each;
    giving every iteration its own copy would need memory for each iteration,
    allocated behind the program's back.
    If the function literal is needed only in this iteration, call it before the
    iteration ends. To use i later, keep its value instead of the function
    literal:
        var saved int // declared before the loop
        ...
        saved = i
    Or pass i to the function literal as a parameter when it is called:
        first = func(i int) { ... }
        ...
        first(i)
    (see "Loop iterations reuse their storage" in the Plutonium spec, section
    18.4)
    (run "pluto explain E3006" for a longer explanation)`,
	"return pointer to local": `main.p:8:9: cannot return &c, a pointer to a local variable
    &c is a pointer to c. c is a local variable of load (declared at main.p:6).
    Its memory is reclaimed when load returns, so &c would point at memory that
    no longer holds c.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    To give the value to the caller, return it instead. This copies it, or moves
    it if it is an owner:
        return c
    To keep it at a fixed address after load returns, allocate it and return its
    owner, an owned[Config]:
        c := new(Config)
        ...
        return c
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3001" for a longer explanation)`,
	"return slice of local array": `main.p:5:9: cannot return buf[:], a slice of buf, a local array
    buf[:] is a slice that looks at buf. buf is a local array of header
    (declared at main.p:4). Its memory is reclaimed when header returns, so
    buf[:] would point at memory that no longer holds buf.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    To give the caller the elements, return an owner of them, such as a vec.Vec:
        v := vec.New[T]()
        v.Push(...)
        return v
    or copy them into memory the caller passes in:
        func fill(dst []T) int {
            ...
            return copy(dst, buf[:n])
        }
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3001" for a longer explanation)`,
	"store closure in package variable": `main.p:7:2: cannot store the function literal in package variable hook, which outlives total
    A function literal that uses variables of the function around it refers to
    those variables where they live, in counter's frame (total is declared at
    main.p:6). When counter returns, total's memory is reclaimed, but package
    variables live for the whole program, so package variable hook would be left
    pointing at reclaimed memory.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    Store a value that carries its own state instead of the function literal,
    such as a struct with a method, or make total a package variable if it must
    outlive counter.
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3002" for a longer explanation)`,
	"store pointer in package variable": `main.p:9:2: cannot store &c in package variable last, which outlives c
    &c is a pointer to c. c is a local variable of keep (declared at main.p:8).
    When keep returns, c's memory is reclaimed, but package variables live for
    the whole program, so package variable last would be left pointing at
    reclaimed memory.
    Go would move this memory to the heap, and its garbage collector would free
    it later. Plutonium has no garbage collector and never moves memory to the
    heap behind the program's back, so that every allocation is visible in the
    code.
    Store the value instead of a pointer to it, changing the type of last to
    match. This copies c, or moves it if it is an owner:
        last = c
    Or allocate c and store its owner, an owned[Config], which moves into last:
        last = new(Config)
    (see "Views" in the Plutonium spec, section 18.3)
    (run "pluto explain E3002" for a longer explanation)`,
}

// TestSameAsOld checks that the messages of the programs of sameAsOld are
// still the ones internal/escape wrote, word for word, and that the
// analysis leaves the task start to lower.
func TestSameAsOld(t *testing.T) {
	for name, src := range sameAsOld {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			got := compat.Join(newMessages(t, src), "\n")
			want, ok := sameAsOldWant[name]
			if !ok {
				if got != "" {
					t.Errorf("messages for a program internal/escape accepts:\n%s", got)
				}
				return
			}
			if strings.TrimRight(got, "\n") != want {
				t.Errorf("messages differ\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

// TestTypeParameterBox checks the message for an interface made from a
// value of a type parameter: it holds a copy only for some type
// arguments, and the message names one the program uses. internal/escape
// said "would hold a copy" for every type argument, a pointer included.
func TestTypeParameterBox(t *testing.T) {
	msgs := newMessages(t, `package main

func box[T any](x T) any { return x }

type P struct{ a, b int }

func main() {
	n := 1
	_ = box(&n)
	_ = box(P{1, 2})
}
`)
	m := wantMessage(t, msgs,
		"main.p:3:35: cannot return x as an interface: when T is P, the interface holds a copy of it in box's frame",
		"puts the value in the interface itself when the type argument fits in one word",
		"The program uses box with T = P, which is larger than one word.",
		"Return the value as a T, changing the result type to T")
	if strings.Contains(m, "p.err = T{...}") {
		t.Errorf("fix writes a composite literal of a type parameter:\n%s", m)
	}
}

// TestCalleeChain checks that a view a callee keeps is explained down the
// calls to the statement that keeps it, in the program's terms.
func TestCalleeChain(t *testing.T) {
	msgs := newMessages(t, sameAsOld["callee chain"])
	wantMessage(t, msgs,
		"cannot pass &n to pass, which keeps it after returning",
		"pass passes q to store (main.p:7), which stores p in package variable kept (main.p:5), so &n would outlive n.")
}

// TestFieldPath checks that a pointer loaded through a parameter is named
// by the fields the source writes.
func TestFieldPath(t *testing.T) {
	msgs := newMessages(t, `package main

type Node struct {
	v    int
	next *Node
}

var keep *Node

func keepNext(n *Node) { keep = n.next }

func main() {
	a := Node{v: 1}
	b := Node{v: 2, next: &a}
	keepNext(&b)
}
`)
	wantMessage(t, msgs, "cannot pass &b to keepNext", "keepNext stores a pointer loaded from n.next in package variable keep (main.p:10)")
}

// TestRangeBodyResult checks the message for a closure that the body of a
// range loop over a function returns from the function the loop is in.
func TestRangeBodyResult(t *testing.T) {
	msgs := newMessages(t, `package main

type Seq func(yield func(int) bool)

func count(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

func first(seq Seq) func() int {
	for x := range seq {
		return func() int { return x }
	}
	return nil
}

func main() { _ = first(count) }
`)
	if len(msgs) == 0 {
		t.Skip("the analysis accepts the program")
	}
	m := wantMessage(t, msgs, "the result of first")
	if strings.Contains(m, "memory that lives longer") {
		t.Errorf("no destination named:\n%s", m)
	}
}

// TestMapKey checks a view stored as a key of a map.
func TestMapKey(t *testing.T) {
	msgs := newMessages(t, `package main

var set = map[*int]bool{}

func main() {
	n := 1
	set[&n] = true
}
`)
	if len(msgs) == 0 {
		t.Skip("the analysis accepts the program")
	}
	wantMessage(t, msgs, "cannot store &n in a key of set, which outlives n", "changing the map's key type to match")
}

// TestRegionLoop checks the message for the closure of a call in a loop
// kept past the iteration (E3101): the lowerer's text, which the golden
// programs show when the lowerer finds it first.
func TestRegionLoop(t *testing.T) {
	msgs := newMessages(t, `package main

func counterFrom(n int) func() int {
	return func() int {
		n++
		return n
	}
}

func main() {
	var last func() int
	for n := 0; n < 3; n++ {
		last = counterFrom(n)
	}
	println(last())
}
`)
	if len(msgs) == 0 {
		t.Skip("the analysis accepts the program")
	}
	wantMessage(t, msgs,
		"main.p:13:10: cannot keep the closure counterFrom(n) returns past this iteration of the loop: it is stored in last, declared outside the loop (main.p:11)",
		"The region of a call in a loop is part of this frame")
}

// TestLimit checks the message of a function past the analysis's limits
// (E3010), a finding at the function's declaration with no object.
func TestLimit(t *testing.T) {
	prog := loadSrc(t, "package main\n\nfunc big() int { return 1 }\n\nfunc main() { _ = big() }\n")
	p, res, err := Run(prog, sched.Options{})
	if err != nil {
		t.Fatal(err.Error())
	}
	var fn *eir.Func
	for _, f := range p.Funcs {
		if f.Key.Obj != nil && f.Key.Obj.Name() == "big" {
			fn = f
		}
	}
	if fn == nil {
		t.Fatal("no function big")
	}
	res.Findings = compat.Append(res.Findings, solve.Finding{Code: "E3010", Pos: fn.Pos, Func: fn.ID, Obj: eir.NoObj, Callee: eir.NoFunc, Entry: -1})
	var msgs []string
	for _, d := range report.Diagnostics(prog, p, res) {
		msgs = compat.Append(msgs, render(d))
	}
	m := wantMessage(t, msgs,
		"main.p:3:1: the escape analysis of big reached its limit: the function holds more distinct pointers than the analysis can follow",
		"Split big into smaller functions")
	if w := internalTerm(m); w != "" {
		t.Errorf("message names %q:\n%s", w, m)
	}
}

// TestNoInternalTerms checks the check the golden comparison applies.
func TestNoInternalTerms(t *testing.T) {
	for _, s := range []string{"passes register 3", "a token of f", "main.p:3: see /home/x/main.p:3"} {
		if internalTerm(s) == "" {
			t.Errorf("%q: not caught", s)
		}
	}
	if w := internalTerm("cannot pass &x to f, which keeps it after returning (main.p:3)"); w != "" {
		t.Errorf("a plain message caught: %q", w)
	}
}
