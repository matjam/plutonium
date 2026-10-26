package main

import "unsafe"

type Node owner struct {
	val  int
	next owned[Node]
}

func (n *Node) Val() int { return n.val }

type Style struct{ size int }

func (s *Style) Apply() int { return s.size }

type Widget owner struct {
	owned[Style] // embedded box: Style's methods are promoted
}

type Panel struct {
	shared[Style] // a shared box copies and counts
}

type Valuer interface{ Val() int }

func use() {
	n := new(Node)
	n.val = 1
	n.next = new(Node)
	_ = n.next.val + n.Val() + (*n).val
	var v *Node = (*Node)(n)
	_ = &*n == v
	_ = n == nil
	var e owned[Node]
	_ = e != nil
	_ = unsafe.Pointer(n)
	var w Widget
	_ = w.Apply() + w.size
	var p Panel
	_ = p.Apply()
	var vi Valuer = v
	_ = vi
	arr := new([3]int)
	arr[1] = 2
	_ = len(arr) + arr[1]
	for i, x := range arr {
		_, _ = i, x
	}
	_ = arr[:]
	var s shared[Node]
	wk := weak[shared[Node]](s)
	s2, ok := wk.Upgrade()
	_, _ = s2, ok
	var sy synced[Node]
	_ = weak[synced[Node]](sy)
}
