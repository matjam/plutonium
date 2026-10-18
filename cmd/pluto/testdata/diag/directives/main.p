package main

//pu:interupt
func timer() {}

//pu:alloc
func build() string { return "a" + "b" }

//pu:noreturn
func halt() {
	for {
	}
}

//pu:percpu
func notAVariable() {}

//pu:inline
var notAFunction int

//pu:nobounds
func oldName() {}

//pu:nochecks
func unchecked() {}

//pu:noinline because it is hot
func hot() {}

//pu:interrupt fast
func fastIRQ() {}

func main() {
	//pu:noinline
	println(build())
	timer()
	halt()
}
