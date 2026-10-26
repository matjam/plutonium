package main
import ("pluto/rt";"unsafe")
type waiting struct{}
func(*waiting)Alloc(size,align uintptr)unsafe.Pointer{return nil}
func(*waiting)Free(p unsafe.Pointer,size,align uintptr){rt.Park()}
var parent waiting
//pu:panic
func onPanic(p *rt.Panic){rt.CatchAllocFree(&parent,nil,0,1);rt.Abort()}
func main(){}
