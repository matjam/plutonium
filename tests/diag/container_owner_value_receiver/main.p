// x[k] and len(x) borrow the container (language spec sections 11.11 and
// 13.6). On an owner type, a method with a value receiver consumes the
// owner (section 10.14), so it cannot supply them: E2452 for each of
// Index, SetIndex, IndexOk and Len.
package main

type Reader owner struct{ n int }

func (r *Reader) Drop()          {}
func (r Reader) Index(k int) int { return r.n + k }

type Writer owner struct{ n int }

func (w *Writer) Drop()                {}
func (w Writer) SetIndex(k int, v int) {}

type Looker owner struct{ n int }

func (l *Looker) Drop()                    {}
func (l Looker) IndexOk(k int) (int, bool) { return l.n + k, true }

type Sizer owner struct{ n int }

func (s *Sizer) Drop()   {}
func (s Sizer) Len() int { return s.n }

func main() {
	r := Reader{n: 1}
	println(r[1])
	w := Writer{n: 2}
	w[1] = 3
	l := Looker{n: 4}
	v, ok := l[1]
	println(v, ok)
	s := Sizer{n: 5}
	println(len(s))
}
