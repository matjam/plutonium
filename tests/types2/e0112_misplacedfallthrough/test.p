package main

func typename(i interface{}) string {
	switch i.(type) {
	case int64:
		fallthrough
	case int:
		return "int"
	}
	return "unsupported"
}

func main() {}
