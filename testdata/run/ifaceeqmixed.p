package main

type Node interface{ Pos() int }

type Expr interface {
	Pos() int
	expr()
}

type Stmt struct{}

func (*Stmt) Pos() int { return 1 }

type Ident struct{}

func (*Ident) Pos() int { return 2 }
func (*Ident) expr()    {}

// Interfaces of two types compare in the one the other converts to: an
// Expr compared with a Node that is not an Expr is unequal, not a failed
// conversion.
func same(e Expr, n Node) bool { return e == n }

func main() {
	s, id := &Stmt{}, &Ident{}
	var n Node = id
	println(same(id, s), same(id, id), same(id, n), n != Expr(id))
}
