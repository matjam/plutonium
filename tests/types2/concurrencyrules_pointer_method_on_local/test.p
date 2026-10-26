package main
type W struct{ n int }
func (w *W) M() {}
func g() { var w W; task w.M() }