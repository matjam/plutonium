package main

type Frame owner uintptr

func (f *Frame) Drop() {}

func f(n uint32, p uintptr) (Frame, Frame, Frame) { return Frame(0), Frame(n), Frame(p) }

func g(fr *Frame) (uint64, float64) { return uint64(*fr), float64(*fr) }
