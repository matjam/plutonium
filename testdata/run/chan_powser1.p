// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Adapted from Go's test/chan/powser1.go for the common subset of Go and
// Plutonium: each generator that was a function literal started with task
// (and captured its variables) is a named function that takes them as
// arguments, the printing mode selected by os.Args and the unused
// floating-point evaluator are left out, and the check prints its results.
// The data races of the original are removed: the request counter seqno
// and the channel-name counter, which every generator wrote without a lock,
// are gone (requests carry 0; nothing read the names), and each generation
// of split gets its own copy of the pair of output channels instead of
// sharing one array that the next generation renames. A demand channel is
// a value (its two channels) rather than a pointer to one, so nothing has
// to own or free it.

// Test concurrency primitives: power series.

// Power series package
// A power series is a channel, along which flow rational
// coefficients.  A denominator of zero signifies the end.
// Original code in Newsqueak by Doug McIlroy.
// See Squinting at Power Series by Doug McIlroy,
//   https://swtch.com/~rsc/thread/squint.pdf

package main

// The checked coefficients are finite even though the series are lazy and
// unbounded. Stop their demand network explicitly before normal shutdown joins
// its tasks. Cancellation never changes a coefficient that main checks.
var stopping = make(chan bool)

func sendValue[E any](ch chan<- E, v E) bool {
	select {
	case <-stopping:
		return false
	default:
	}
	select {
	case ch <- v:
		return true
	case <-stopping:
		return false
	}
}

func receiveValue[E any](ch <-chan E) (E, bool) {
	var zero E
	select {
	case <-stopping:
		return zero, false
	default:
	}
	select {
	case v := <-ch:
		return v, true
	case <-stopping:
		return zero, false
	}
}

type rat struct {
	num, den int64 // numerator, denominator
}

func (u rat) pr() {
	if u.den == 1 {
		print(u.num)
	} else {
		print(u.num, "/", u.den)
	}
	print(" ")
}

func (u rat) eq(c rat) bool {
	return u.num == c.num && u.den == c.den
}

type dch struct {
	req chan int
	dat chan rat
}

type dch2 [2]dch

func mkdch() dch {
	return dch{req: make(chan int), dat: make(chan rat)}
}

func mkdch2() dch2 {
	return dch2{mkdch(), mkdch()}
}

// split reads a single demand channel and replicates its
// output onto two, which may be read at different rates.
// A process is created at first demand for a rat and dies
// after the rat has been sent to both outputs.

// When multiple generations of split exist, the newest
// will service requests on one channel, which is
// always renamed to be out[0]; the oldest will service
// requests on the other channel, out[1].  All generations but the
// newest hold queued data that has already been sent to
// out[0].  When data has finally been sent to out[1],
// a signal on the release-wait channel tells the next newer
// generation to begin servicing out[1].

func dosplit(in dch, out dch2, wait chan int) {
	both := false // do not service both channels

	select {
	case <-stopping:
		return
	case <-out[0].req:

	case <-wait:
		both = true
		select {
		case <-stopping:
			return
		case <-out[0].req:

		case <-out[1].req:
			out[0], out[1] = out[1], out[0]
		}
	}

	if !sendValue(in.req, 0) {
		return
	}
	release := make(chan int)
	task dosplit(in, out, release)
	dat, ok := receiveValue(in.dat)
	if !ok {
		return
	}
	if !sendValue(out[0].dat, dat) {
		return
	}
	if !both {
		if _, ok := receiveValue(wait); !ok {
			return
		}
	}
	if _, ok := receiveValue(out[1].req); !ok {
		return
	}
	if !sendValue(out[1].dat, dat) {
		return
	}
	if !sendValue(release, 0) {
		return
	}
}

func split(in dch, out dch2) {
	release := make(chan int)
	task dosplit(in, out, release)
	if !sendValue(release, 0) {
		return
	}
}

func put(dat rat, out dch) bool {
	if _, ok := receiveValue(out.req); !ok {
		return false
	}
	return sendValue(out.dat, dat)
}

func get(in dch) rat {
	if !sendValue(in.req, 0) {
		return finis
	}
	v, ok := receiveValue(in.dat)
	if !ok {
		return finis
	}
	return v
}

// Get one rat from each of n demand channels

func getn(in []dch) [2]rat {
	n := len(in)
	if n != 2 {
		panic("bad n in getn")
	}
	var req [2]chan int
	var dat [2]chan rat
	var out [2]rat
	var i int
	var it rat
	for i = 0; i < n; i++ {
		req[i] = in[i].req
		dat[i] = nil
	}
	for n = 2 * n; n > 0; n-- {

		select {
		case <-stopping:
			return out
		case req[0] <- 0:
			dat[0] = in[0].dat
			req[0] = nil
		case req[1] <- 0:
			dat[1] = in[1].dat
			req[1] = nil
		case it = <-dat[0]:
			out[0] = it
			dat[0] = nil
		case it = <-dat[1]:
			out[1] = it
			dat[1] = nil
		}
	}
	return out
}

// Get one rat from each of 2 demand channels

func get2(in0 dch, in1 dch) [2]rat {
	return getn([]dch{in0, in1})
}

func copy(in dch, out dch) {
	for {
		if _, ok := receiveValue(out.req); !ok {
			return
		}
		if !sendValue(out.dat, get(in)) {
			return
		}
	}
}

func repeat(dat rat, out dch) {
	for {
		if !put(dat, out) {
			return
		}
	}
}

type PS = dch    // power series
type PS2 = [2]PS // pair of power series

var Ones PS
var Twos PS

func mkPS() dch {
	return mkdch()
}

func mkPS2() dch2 {
	return mkdch2()
}

// Conventions
// Upper-case for power series.
// Lower-case for rationals.
// Input variables: U,V,...
// Output variables: ...,Y,Z

// Integer gcd; needed for rational arithmetic

func gcd(u, v int64) int64 {
	if u < 0 {
		return gcd(-u, v)
	}
	if u == 0 {
		return v
	}
	return gcd(v%u, u)
}

// Make a rational from two ints and from one int

func i2tor(u, v int64) rat {
	g := gcd(u, v)
	var r rat
	if v > 0 {
		r.num = u / g
		r.den = v / g
	} else {
		r.num = -u / g
		r.den = -v / g
	}
	return r
}

func itor(u int64) rat {
	return i2tor(u, 1)
}

var zero rat
var one rat

// End mark and end test

var finis rat

func end(u rat) int64 {
	if u.den == 0 {
		return 1
	}
	return 0
}

// Operations on rationals

func add(u, v rat) rat {
	g := gcd(u.den, v.den)
	return i2tor(u.num*(v.den/g)+v.num*(u.den/g), u.den*(v.den/g))
}

func mul(u, v rat) rat {
	g1 := gcd(u.num, v.den)
	g2 := gcd(u.den, v.num)
	var r rat
	r.num = (u.num / g1) * (v.num / g2)
	r.den = (u.den / g2) * (v.den / g1)
	return r
}

func neg(u rat) rat {
	return i2tor(-u.num, u.den)
}

func sub(u, v rat) rat {
	return add(u, neg(v))
}

func inv(u rat) rat { // invert a rat
	if u.num == 0 {
		panic("zero divide in inv")
	}
	return i2tor(u.den, u.num)
}

// Print n terms of a power series
func printn(U PS, n int) {
	done := false
	for ; !done && n > 0; n-- {
		u := get(U)
		if end(u) != 0 {
			done = true
		} else {
			u.pr()
		}
	}
	print(("\n"))
}

// Power-series constructors return channels on which power
// series flow.  They start an encapsulated generator that
// puts the terms of the series on the channel.

// Make a pair of power series identical to a given power series

func Split(U PS) dch2 {
	UU := mkdch2()
	task split(U, UU)
	return UU
}

// Add two power series
func Add(U, V PS) PS {
	Z := mkPS()
	task add2(U, V, Z)
	return Z
}

func add2(U, V, Z PS) {
	var uv [2]rat
	for {
		if _, ok := receiveValue(Z.req); !ok {
			return
		}
		uv = get2(U, V)
		switch end(uv[0]) + 2*end(uv[1]) {
		case 0:
			if !sendValue(Z.dat, add(uv[0], uv[1])) {
				return
			}
		case 1:
			if !sendValue(Z.dat, uv[1]) {
				return
			}
			copy(V, Z)
		case 2:
			if !sendValue(Z.dat, uv[0]) {
				return
			}
			copy(U, Z)
		case 3:
			if !sendValue(Z.dat, finis) {
				return
			}
		}
	}
}

// Multiply a power series by a constant
func Cmul(c rat, U PS) PS {
	Z := mkPS()
	task cmul(c, U, Z)
	return Z
}

func cmul(c rat, U, Z PS) {
	done := false
	for !done {
		if _, ok := receiveValue(Z.req); !ok {
			return
		}
		u := get(U)
		if end(u) != 0 {
			done = true
		} else {
			if !sendValue(Z.dat, mul(c, u)) {
				return
			}
		}
	}
	if !sendValue(Z.dat, finis) {
		return
	}
}

// Subtract

func Sub(U, V PS) PS {
	return Add(U, Cmul(neg(one), V))
}

// Multiply a power series by the monomial x^n

func Monmul(U PS, n int) PS {
	Z := mkPS()
	task monmul(U, n, Z)
	return Z
}

func monmul(U PS, n int, Z PS) {
	for ; n > 0; n-- {
		if !put(zero, Z) {
			return
		}
	}
	copy(U, Z)
}

// Multiply by x

func Xmul(U PS) PS {
	return Monmul(U, 1)
}

func Rep(c rat) PS {
	Z := mkPS()
	task repeat(c, Z)
	return Z
}

// Monomial c*x^n

func Mon(c rat, n int) PS {
	Z := mkPS()
	task mon(c, n, Z)
	return Z
}

func mon(c rat, n int, Z PS) {
	if c.num != 0 {
		for ; n > 0; n = n - 1 {
			if !put(zero, Z) {
				return
			}
		}
		if !put(c, Z) {
			return
		}
	}
	if !put(finis, Z) {
		return
	}
}

func Shift(c rat, U PS) PS {
	Z := mkPS()
	task shift(c, U, Z)
	return Z
}

func shift(c rat, U, Z PS) {
	if !put(c, Z) {
		return
	}
	copy(U, Z)
}

// Multiply. The algorithm is
//	let U = u + x*UU
//	let V = v + x*VV
//	then UV = u*v + x*(u*VV+v*UU) + x*x*UU*VV

func Mul(U, V PS) PS {
	Z := mkPS()
	task mul2(U, V, Z)
	return Z
}

func mul2(U, V, Z PS) {
	if _, ok := receiveValue(Z.req); !ok {
		return
	}
	uv := get2(U, V)
	if end(uv[0]) != 0 || end(uv[1]) != 0 {
		if !sendValue(Z.dat, finis) {
			return
		}
	} else {
		if !sendValue(Z.dat, mul(uv[0], uv[1])) {
			return
		}
		UU := Split(U)
		VV := Split(V)
		W := Add(Cmul(uv[0], VV[0]), Cmul(uv[1], UU[0]))
		if _, ok := receiveValue(Z.req); !ok {
			return
		}
		if !sendValue(Z.dat, get(W)) {
			return
		}
		copy(Add(W, Mul(UU[1], VV[1])), Z)
	}
}

// Differentiate

func Diff(U PS) PS {
	Z := mkPS()
	task diff(U, Z)
	return Z
}

func diff(U, Z PS) {
	if _, ok := receiveValue(Z.req); !ok {
		return
	}
	u := get(U)
	if end(u) == 0 {
		done := false
		for i := 1; !done; i++ {
			u = get(U)
			if end(u) != 0 {
				done = true
			} else {
				if !sendValue(Z.dat, mul(itor(int64(i)), u)) {
					return
				}
				if _, ok := receiveValue(Z.req); !ok {
					return
				}
			}
		}
	}
	if !sendValue(Z.dat, finis) {
		return
	}
}

// Integrate, with const of integration
func Integ(c rat, U PS) PS {
	Z := mkPS()
	task integ(c, U, Z)
	return Z
}

func integ(c rat, U, Z PS) {
	if !put(c, Z) {
		return
	}
	done := false
	for i := 1; !done; i++ {
		if _, ok := receiveValue(Z.req); !ok {
			return
		}
		u := get(U)
		if end(u) != 0 {
			done = true
		}
		if !sendValue(Z.dat, mul(i2tor(1, int64(i)), u)) {
			return
		}
	}
	if !sendValue(Z.dat, finis) {
		return
	}
}

// Binomial theorem (1+x)^c

func Binom(c rat) PS {
	Z := mkPS()
	task binom(c, Z)
	return Z
}

func binom(c rat, Z PS) {
	n := 1
	t := itor(1)
	for c.num != 0 {
		if !put(t, Z) {
			return
		}
		t = mul(mul(t, c), i2tor(1, int64(n)))
		c = sub(c, one)
		n++
	}
	if !put(finis, Z) {
		return
	}
}

// Reciprocal of a power series
//	let U = u + x*UU
//	let Z = z + x*ZZ
//	(u+x*UU)*(z+x*ZZ) = 1
//	z = 1/u
//	u*ZZ + z*UU +x*UU*ZZ = 0
//	ZZ = -UU*(z+x*ZZ)/u

func Recip(U PS) PS {
	Z := mkPS()
	task recip(U, Z)
	return Z
}

func recip(U, Z PS) {
	ZZ := mkPS2()
	if _, ok := receiveValue(Z.req); !ok {
		return
	}
	z := inv(get(U))
	if !sendValue(Z.dat, z) {
		return
	}
	split(Mul(Cmul(neg(z), U), Shift(z, ZZ[0])), ZZ)
	copy(ZZ[1], Z)
}

// Exponential of a power series with constant term 0
// (nonzero constant term would make nonrational coefficients)
// bug: the constant term is simply ignored
//	Z = exp(U)
//	DZ = Z*DU
//	integrate to get Z

func Exp(U PS) PS {
	ZZ := mkPS2()
	split(Integ(one, Mul(ZZ[0], Diff(U))), ZZ)
	return ZZ[1]
}

// Substitute V for x in U, where the leading term of V is zero
//	let U = u + x*UU
//	let V = v + x*VV
//	then S(U,V) = u + VV*S(V,UU)
// bug: a nonzero constant term is ignored

func Subst(U, V PS) PS {
	Z := mkPS()
	task subst(U, V, Z)
	return Z
}

func subst(U, V, Z PS) {
	VV := Split(V)
	if _, ok := receiveValue(Z.req); !ok {
		return
	}
	u := get(U)
	if !sendValue(Z.dat, u) {
		return
	}
	if end(u) == 0 {
		if end(get(VV[0])) != 0 {
			if !put(finis, Z) {
				return
			}
		} else {
			copy(Mul(VV[0], Subst(U, VV[1])), Z)
		}
	}
}

// Monomial Substitution: U(c x^n)
// Each Ui is multiplied by c^i and followed by n-1 zeros

func MonSubst(U PS, c0 rat, n int) PS {
	Z := mkPS()
	task monSubst(U, c0, n, Z)
	return Z
}

func monSubst(U PS, c0 rat, n int, Z PS) {
	c := one
	for {
		if _, ok := receiveValue(Z.req); !ok {
			return
		}
		u := get(U)
		if !sendValue(Z.dat, mul(u, c)) {
			return
		}
		c = mul(c, c0)
		if end(u) != 0 {
			if !sendValue(Z.dat, finis) {
				return
			}
			break
		}
		for i := 1; i < n; i++ {
			if _, ok := receiveValue(Z.req); !ok {
				return
			}
			if !sendValue(Z.dat, zero) {
				return
			}
		}
	}
}

func Init() {
	zero = itor(0)
	one = itor(1)
	finis = i2tor(1, 0)
	Ones = Rep(one)
	Twos = Rep(itor(2))
}

func check(U PS, c rat, count int, str string) {
	for i := 0; i < count; i++ {
		r := get(U)
		if !r.eq(c) {
			print("got: ")
			r.pr()
			print("should get ")
			c.pr()
			print("\n")
			panic(str)
		}
	}
}

const N = 10

func checka(U PS, a []rat, str string) {
	for i := 0; i < N; i++ {
		check(U, a[i], 1, str)
	}
	print(str, ": ")
	for i := 0; i < N; i++ {
		a[i].pr()
	}
	print("\n")
}

func main() {
	Init()
	check(Ones, one, 5, "Ones")
	check(Add(Ones, Ones), itor(2), 0, "Add Ones Ones") // 1 1 1 1 1
	check(Add(Ones, Twos), itor(3), 0, "Add Ones Twos") // 3 3 3 3 3
	var abuf [N]rat
	a := abuf[:]
	d := Diff(Ones)
	for i := 0; i < N; i++ {
		a[i] = itor(int64(i + 1))
	}
	checka(d, a, "Diff") // 1 2 3 4 5
	in := Integ(zero, Ones)
	a[0] = zero // integration constant
	for i := 1; i < N; i++ {
		a[i] = i2tor(1, int64(i))
	}
	checka(in, a, "Integ")                               // 0 1 1/2 1/3 1/4 1/5
	check(Cmul(neg(one), Twos), itor(-2), 10, "CMul")    // -1 -1 -1 -1 -1
	check(Sub(Ones, Twos), itor(-1), 0, "Sub Ones Twos") // -1 -1 -1 -1 -1
	m := Mul(Ones, Ones)
	for i := 0; i < N; i++ {
		a[i] = itor(int64(i + 1))
	}
	checka(m, a, "Mul") // 1 2 3 4 5
	e := Exp(Ones)
	a[0] = itor(1)
	a[1] = itor(1)
	a[2] = i2tor(3, 2)
	a[3] = i2tor(13, 6)
	a[4] = i2tor(73, 24)
	a[5] = i2tor(167, 40)
	a[6] = i2tor(4051, 720)
	a[7] = i2tor(37633, 5040)
	a[8] = i2tor(43817, 4480)
	a[9] = i2tor(4596553, 362880)
	checka(e, a, "Exp") // 1 1 3/2 13/6 73/24
	at := Integ(zero, MonSubst(Ones, neg(one), 2))
	for c, i := 1, 0; i < N; i++ {
		if i%2 == 0 {
			a[i] = zero
		} else {
			a[i] = i2tor(int64(c), int64(i))
			c *= -1
		}
	}
	checka(at, a, "ATan") // 0 -1 0 -1/3 0 -1/5
	print("printn Ones: ")
	printn(Ones, 10)
	print("printn Exp: ")
	printn(Exp(Ones), 8)
	close(stopping)
}
