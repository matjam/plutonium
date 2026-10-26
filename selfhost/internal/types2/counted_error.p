// Translated by go2pu from internal/types2/counted_error.go at the go-cutover tag; edited by hand since.

package types2

// IsCountedError reports whether T is an ordinary interface that includes
// Error() string. Such interfaces count shared and synced dynamic values
// (language spec section 16.7); other interfaces only lend them.
func IsCountedError(T Type) bool {
	if T == nil || IsOwner(T) {
		return false
	}
	i, ok := T.Underlying().(*Interface)
	if !ok {
		return false
	}
	e := Universe.Lookup("error").Type().Underlying().(*Interface)
	return Implements(i, e)
}
