package compat

// StringOf is fmt's formatting of a pointer with a String method that
// dereferences it (tools/go2pu stringers.go): <nil> for a nil pointer, as
// fmt prints the method's panic, and the method's result otherwise.
func StringOf[T interface {
	comparable
	String() string
}](x T) string {
	var zero T
	if x == zero {
		return "<nil>"
	}
	return x.String()
}

// ErrorOf is StringOf for an Error method.
func ErrorOf[T interface {
	comparable
	Error() string
}](x T) string {
	var zero T
	if x == zero {
		return "<nil>"
	}
	return x.Error()
}
