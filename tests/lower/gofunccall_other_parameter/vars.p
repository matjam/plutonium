package main

import "pluto/sync"

// The wait groups of the case's program in main.p, package variables so
// that the tasks they start may outlive the frames that start them.
var startWG, mainWG sync.WaitGroup
