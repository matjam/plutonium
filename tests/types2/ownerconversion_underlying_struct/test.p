package main

type Frame owner uintptr

func (f *Frame) Drop() {}

func f(s struct{ x int }) Frame { return Frame(s) }
