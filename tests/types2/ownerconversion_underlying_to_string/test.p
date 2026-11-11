package main

type Frame owner uintptr

func (f *Frame) Drop() {}

func f(fr *Frame) string { return string(*fr) }
