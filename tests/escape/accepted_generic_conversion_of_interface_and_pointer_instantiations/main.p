package main

type Node interface{ Pos() int }
type Expr interface {
	Node
	exprNode()
}
type Ident struct{ n int }
func (i *Ident) Pos() int { return i.n }
func (i *Ident) exprNode() {}
type Visitor interface{ Visit(n Node) }
type keeper struct{}
var saved Node
func (keeper) Visit(n Node) { saved = n }
func walkList[N Node](v Visitor, list []N) {
	for _, n := range list {
		v.Visit(n)
	}
}
var exprs = []Expr{&Ident{1}}
var idents = []*Ident{{2}}
func main() {
	walkList(Visitor(keeper{}), exprs)
	walkList(Visitor(keeper{}), idents)
}
