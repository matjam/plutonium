package main
func f() { defer func() { _ = recover() }() }