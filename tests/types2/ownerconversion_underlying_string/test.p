package main

type Frame owner uintptr

func (f *Frame) Drop() {}

func f() Frame { return Frame("abc") }
