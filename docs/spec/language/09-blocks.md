## 9. Blocks

A *block* is a possibly empty sequence of declarations and statements within matching brace brackets.

```
Block         = "{" StatementList "}" .
StatementList = { Statement ";" } .
```

In addition to explicit blocks in the source code, there are implicit blocks:

1. The *universe block* encompasses all Plutonium source text.
2. Each package has a *package block* containing all Plutonium source text for that package.
3. Each file has a *file block* containing all Plutonium source text in that file.
4. Each `if`, `for` and `switch` statement is considered to be in its own implicit block.
5. Each clause in a `switch` or `select` statement acts as an implicit block.
6. Each or clause (section 11.27, Or clauses) is in an implicit block, which declares `err`.

Blocks nest and influence scoping (chapter 10, Declarations and scope).

Blocks also decide the lifetime of what they declare. A variable declared in a block lives in the frame of
its function (section 18.2, Where variables live), and the owners that its variables still hold when the
block ends are dropped, in reverse order of declaration, on every path out of the block: falling off the
end, `return`, `break`, `continue` and `goto` (section 18.6, Drops). The body of a loop is a block that
runs once per iteration and reuses its variables' storage each time (section 18.4, Loop iterations reuse
their storage).

```go
type Res owner struct{ id int }

func (r *Res) Drop() {
	if r.id != 0 { // Drop accepts the zero value, which holds nothing
		println("drop", r.id)
	}
}

func f() {
	a := Res{1}
	{
		b := Res{2}
		println(a.id, b.id)
	} // b is dropped here
	println(a.id)
} // a is dropped here
```

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
