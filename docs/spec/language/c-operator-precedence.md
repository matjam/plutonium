## C. Operator precedence

Unary operators have the highest precedence. As the `++` and `--` operators form statements, not
expressions, they fall outside the operator hierarchy. As a consequence, statement `*p++` is the same as
`(*p)++`.

There are five precedence levels for binary operators. Multiplication operators bind strongest, followed by
addition operators, comparison operators, `&&` (logical AND), and finally `||` (logical OR):

```
Precedence    Operator
    5             *  /  %  <<  >>  &  &^
    4             +  -  |  ^
    3             ==  !=  <  <=  >  >=
    2             &&
    1             ||
```

Binary operators of the same precedence associate from left to right. For instance, `x / y * z` is the same
as `(x / y) * z`.

```
+x                         // x
42 + a - b                 // (42 + a) - b
23 + 3*x[i]                // 23 + (3 * x[i])
x <= f()                   // x <= f()
^a >> b                    // (^a) >> b
f() || g()                 // f() || g()
x == y+1 && <-chanInt > 0  // (x == (y+1)) && ((<-chanInt) > 0)
```

The unary operators are `+`, `-`, `!`, `^`, `*`, `&` and `<-`. What each operator accepts is defined in section 11.18 (Operators) and the sections that
follow it: arithmetic operators (11.19), comparison operators (11.20), logical operators (11.21), address
operators (11.22) and the receive operator (11.23). An operator that does not apply to a kind of type, such
as arithmetic on an enum or on an owner, is a compile error there. It does not change the precedence.

Adapted in part from the Go specification (The Go Authors, CC BY 4.0); see [appendix G](g-attribution.md).
