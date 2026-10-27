package main

import "pluto/vec"

// ints names the element type of the case's program in main.p: an
// explicit type argument vec.Vec[int] in a call is misread as an index
// expression by the moves pass (tests/moves/type_argument_of_owner_vec).
type ints = vec.Vec[int]
