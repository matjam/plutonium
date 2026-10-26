// Translated by go2pu from internal/escape/escapes.go at the go-cutover tag; edited by hand since.

package escape

import (
	"pluto/strings"
	"selfhost/compat"
	"selfhost/internal/syntax/ast"
	"selfhost/internal/syntax/token"
	"selfhost/internal/types2"
	"selfhost/pu"
)

// Applying the escapes of a callee.
//
// A callee's summary lists the pointers it keeps (escapes): a key says "what
// is found by following this path from parameter i". A call keeps what the
// keys lead to from the pointers passed. The summaries of translated code
// have hundreds of thousands of keys, and the passes that compute them apply
// every key of every callee at every call, again and again, although a key
// leads from a pointer to the same place every time.
//
// Following a key from a pointer that stands for a parameter of the caller
// (a tParam token) depends on nothing but the pointer and the key, and what
// it finds is recorded in the caller's own summary, where finding it again
// changes nothing. So a call site remembers, for each such pointer, how many
// of the callee's keys it has followed from it (siteState), and applies only
// the keys added since. The keys of a parameter are kept in the order they
// were added (summary.escLog) for that. Pointers to the frame are followed
// from every time: what lies behind them depends on what the frame holds,
// which changes while the function is walked. Pointers to the heap lead to
// the heap, which keeps nothing, except in the pass that reports errors.
//
// In the pass that reports errors, the pointers to the frame are the ones
// that matter: what the parameter pointers find is in the summaries already.

// A siteKey names a call, of one callee in one way.
type siteKey struct {
	site ast.Node
	f    *function
	mode recvMode
}

// A siteState remembers, for each parameter of the callee, how many of its
// keys have been followed from each pointer passed there.
type siteState struct {
	done []map[ptKey]int
}

// A ptKey identifies a parameter pointer apart from the walker that made it:
// scopes are made again for every walk, but belong to one function.
type ptKey struct {
	fn                             *function
	counted, directCount, borrowed bool
	ge                             bool
	shape, filtShape               ptrShape
	idx, filtAt                    int
	path, tag                      string
	filt                           types2.Type
}

func ptKeyOf(t tok) ptKey {
	return ptKey{fn: t.sc.fn, counted: t.counted, directCount: t.directCount, borrowed: t.borrowed, ge: t.ge,
		shape: t.shape, filtShape: t.filtShape, idx: t.idx, filtAt: t.filtAt, path: t.path, tag: t.tag, filt: t.filt}
}

// applyEscapes follows the keys of f's escapes that a call has not followed
// from the pointers it passes, and records what they keep.
func (w *walker) applyEscapes(f *function, args []tset, cs *callSite, pos token.Pos, label string, mode recvMode, argOf *argEval) {
	sum := &f.sum
	var st *siteState
	var base *viewErr
	act := ""
	// The buffer of fastKey, whose key is used before the next is derived.
	// Declared once per call, not for every key: Go moves it to the heap.
	buf := pu.Alloc[[64]byte]()
	for i := range sum.escLog {
		keys := &sum.escLog[i]
		if i >= len(args) || keys.len() == 0 || len(args[i]) == 0 {
			continue
		}
		n := keys.len()
		for _, t := range args[i] {
			done := 0
			switch t.kind {
			case tHeap:
				continue
			case tParam:
				if st == nil {
					st = w.siteState(cs, f, mode)
				}
				for len(st.done) <= i {
					st.done = compat.Append(st.done, nil)
				}
				if st.done[i] == nil {
					st.done[i] = map[ptKey]int{}
				}
				done = st.done[i][ptKeyOf(t)]
			}
			if done >= n {
				continue
			}
			if base == nil {
				base = pu.New(viewErr{sink: sinkCall, pos: pos, fn: w.scopeName(w.sc), callee: label})
				act = compat.Concat("passes %s to ", label)
			}
			if t.kind == tFrame && !w.reachesParam(t.loc) {
				// Nothing this pointer leads to can be a parameter's.
				continue
			}
			structParam := t.kind == tParam && i < len(f.params) && isStructValue(f.params[i].Type())
			one := pu.New([1]tok{t})
			for j := done; j < n; j++ {
				e := keys.at(j)
				if t.kind == tParam && !e.k.counts {
					// A pointer to a parameter of the caller leads to one
					// pointer or none, and only its key is needed.
					key, ok, once := w.fastKey(f, i, mode, structParam, t, e.k, buf)
					if checkFast {
						w.checkFastKey(argOf, e.k, (*one)[:], key, ok)
					}
					if ok {
						w.escapeKey(t, key, act, base.pos, &e.r)
					}
					if once {
						break // every key leads to the same pointer
					}
					continue
				}
				v := argOf.of(e.k, (*one)[:])
				if len(v) == 0 {
					continue
				}
				w.sinkCallee(v, base, act, &e.r, cs.expr(e.k.idx))
			}
			if t.kind == tParam {
				st.done[i][ptKeyOf(t)] = n
			}
		}
	}
}

// fastKey is the key under which following k from the parameter pointer t
// is recorded, as argOf computes the pointer: ok is false if the pointer is
// not where the key leads from. once says that every key leads to the same
// pointer.
func (w *walker) fastKey(f *function, i int, mode recvMode, structParam bool, t tok, k pkey, buf *[64]byte) (key pkey, ok, once bool) {
	preLoad := false
	if i == 0 {
		switch mode {
		case recvReach:
			return keyOf(t.untag().widen()), true, true
		case recvBoxed:
			preLoad, t = true, t.untag()
			k.borrowed = false
		}
	}
	if structParam {
		var okp bool
		if t, k, okp = w.structPrelude(t, k, f.params[i].Type()); !okp {
			return pkey{}, false, false
		}
	}
	return w.deriveKey(t, k, preLoad, buf), true, false
}

// reachesParam reports whether what the frame location l holds, directly or
// through other locations of the frame, includes a pointer to a parameter.
func (w *walker) reachesParam(l *loc) bool {
	if l.kind == lkObject || l.kind == lkArena || l.object {
		return false // what they hold is the heap
	}
	seen := map[*loc]bool{l: true}
	stack := []*loc{l}
	for len(stack) > 0 {
		l := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		found := false
		l.each(func(set tset) bool {
			for _, t := range set {
				switch t.kind {
				case tParam:
					found = true
					return false
				case tFrame:
					if !seen[t.loc] && t.loc.kind != lkObject && t.loc.kind != lkArena && !t.loc.object {
						seen[t.loc] = true
						stack = compat.Append(stack, t.loc)
					}
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

func (w *walker) siteState(cs *callSite, f *function, mode recvMode) *siteState {
	if w.a.sites == nil {
		w.a.sites = map[siteKey]*siteState{}
	}
	k := siteKey{cs.site, f, mode}
	st := w.a.sites[k]
	if st == nil {
		st = pu.New(siteState{})
		w.a.sites[k] = st
	}
	return st
}

// sinkCallee is sink for a callee that keeps pointers for reason r.
func (w *walker) sinkCallee(v tset, base *viewErr, act string, r *reason, expr ast.Expr) {
	for _, t := range v {
		switch t.kind {
		case tFrame:
			ve := *base
			ve.expr, ve.calleeWhy, ve.l = expr, r, t.loc
			w.report(&ve, compat.Sprintf("sink|%p", t.loc))
		case tParam:
			if w.escapeKnown(t) {
				continue
			}
			w.escapeCall(t, act, base.pos, r)
		}
	}
}

// escapeCall records that the parameter pointer t escapes because the callee
// keeps what it leads to, for reason r.
func (w *walker) escapeCall(t tok, act string, pos token.Pos, r *reason) {
	if t.sc.level == 0 && t.sc.fn.resultHolds[t.idx] {
		return
	}
	sum := &t.sc.fn.sum
	k := keyOf(t)
	if sum.hasEscape(k) {
		return
	}
	sum.addEscape(k, w.callReason(t.sc.fn, act, pos, r))
	w.a.grew = true
}

// escapeKey is escapeCall for the key a parameter pointer t is recorded
// under, whose path lies in a buffer that is reused.
func (w *walker) escapeKey(t tok, key pkey, act string, pos token.Pos, r *reason) {
	if t.sc.level == 0 && t.sc.fn.resultHolds[t.idx] {
		return
	}
	sum := &t.sc.fn.sum
	if sum.hasEscape(key) {
		return
	}
	key.path = strings.Clone(key.path)
	sum.addEscape(key, w.callReason(t.sc.fn, act, pos, r))
	w.a.grew = true
}

// reportEscapes is applyEscapes in the pass that reports errors: it follows
// every key, in order, from the pointers that are not parameters'.
func (w *walker) reportEscapes(f *function, args []tset, passErr func(int, *reason) *viewErr, label string, argOf *argEval) {
	var raw []tset
	any := false
	for _, a := range args {
		var r tset
		for _, t := range a {
			if t.kind != tParam {
				r = tset(compat.Append(r, t))
			}
		}
		raw = compat.Append(raw, r)
		any = any || len(r) > 0
	}
	if !any {
		return
	}
	for _, k := range f.sum.sortedEscapes() {
		if k.idx >= len(raw) || len(raw[k.idx]) == 0 {
			continue
		}
		r := f.sum.escapeReason(k)
		v := argOf.of(k, raw[k.idx])
		if len(v) == 0 {
			continue
		}
		w.sinkTail(v, passErr(k.idx, r), compat.Concat("passes %s to ", label), pu.New(goClosure_reportEscapes_1{r: r}))
	}
}

// checkFast makes applyEscapes check fastKey against the general way
// (set by the tests).
var checkFast = compat.Getenv("PLUTO_ESCAPE_CHECK") != ""

func (w *walker) checkFastKey(argOf *argEval, k pkey, one tset, key pkey, ok bool) {
	v := argOf.of(k, one)
	if ok != (len(v) == 1) || ok && (v[0].kind != tParam || v[0].sc.fn != one[0].sc.fn || keyOf(v[0]) != key) {
		panic(pu.Box(compat.Sprintf("fastKey(%+v, %+v) = %+v, %v; the general way gives %+v", one[0], k, key, ok, v)))
	}
}

// goClosure_reportEscapes_1 is the function literal at escapes.go:287, converted to a struct
// that holds the variables it captures (go2pu closure conversion).
type goClosure_reportEscapes_1 struct {
	r *reason
}

func (goSelf *goClosure_reportEscapes_1) Call() string {
	return compat.Concat(", which ", goSelf.r.String())
}
