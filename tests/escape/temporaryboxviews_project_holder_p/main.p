package main
import "pluto/alloc"
type Item struct { x int; a [2]int; p *int }
type Holder owner struct { p owned[Item] }
type SharedHolder struct { p synced[Item] }
type Other struct { x int }
func (i *Other) Get() *int { return &i.x }
type Pair struct { p synced[Item]; q synced[Other] }
type Getter interface { Get() *int }
func (i *Item) Get() *int { return &i.x }
func project(g Getter) *int { return g.Get() }
func genericProject[T Getter](g T) *int { return g.Get() }
var saved *int
var strong synced[Item]
func saveStrong[T synced[Item]](g T) { strong = synced[Item](g) }
func save[T Getter](g T) { saved = g.Get() }
func set[T Getter](g T, dst **int) { *dst = g.Get() }
func aggregateProject[T Getter](h Wrap[T]) *int { return h.p.Get() }
func deepProject[T Getter](h Wrap[Wrap[Wrap[T]]]) *int { return h.p.p.p.Get() }
func (i *Item) Identity() *Item { return i }
func identity[T interface{ Identity() *Item }](g T) *Item { return g.Identity() }
func (i *Item) Raw() *int { return i.p }
func raw[T interface{ Raw() *int }](g T) *int { return g.Raw() }
func tuple() (synced[Item], int) { return alloc.Synced[Item](), 1 }
func tupleProject[T Getter](g T, n int) *int { return g.Get() }
func arrayProject[T Getter](g [1]T) *int { return g[0].Get() }
func indirect(g Getter) *int { f := g.Get; return f() }
type Wrap[T any] struct { p T }
func wrap[T any](p T) Wrap[T] { return Wrap[T]{p} }
func pair() Pair { return Pair{alloc.Synced[Item](), alloc.Synced[Other]()} }
func choose(a, b Getter) *int { if a != nil { return a.Get() }; return b.Get() }
func mk() owned[Item] { return new(Item{x: 7}) }
func holder() Holder { return Holder{p: mk()} }
func array() [1]owned[Item] { return [1]owned[Item]{mk()} }
func refs() [1]synced[Item] { return [1]synced[Item]{alloc.Synced[Item]()} }
func sharedHolder() SharedHolder { return SharedHolder{alloc.Synced[Item]()} }
func forward[T owner any](v T) T { return v }
func relay[T owner any](v T) T { return forward(v) }
func copyValue[T any](v T, dst *T) { *dst = v }
func relayCopy[T any](v T, dst *T) { copyValue(v, dst) }
type Mixed struct { a Wrap[synced[Item]]; b Wrap[synced[Other]] }

func read(p *int) int { return *p }
func main() { p := project(holder().p); println(*p) }