package main

// owner followed by "]" is the name of the constraint, and no type named
// owner is declared here (language spec section 4.6).
func F[T owner](x T) T { return x }
