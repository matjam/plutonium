package main

func _() {
	var a struct{ _ func() }
	switch a {
	}
}

func main() {}
