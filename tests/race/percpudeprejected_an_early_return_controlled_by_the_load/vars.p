package main

// The threads the case's program in main.p points at, as package
// variables, so that they outlive main's frame.
var thread0, thread1, thread3 = Thread{id: 0}, Thread{id: 1}, Thread{id: 3}
