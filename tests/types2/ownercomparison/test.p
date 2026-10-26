package main

type Fd owner int32

func (f *Fd) Drop() {}

func same(f, g *Fd) bool { return *f == *g }
