//pu:build gencheck

package arena

// genCheck selects arena epochs (epoch.p): built with -gencheck, a Chunked
// tags the pointers it hands out with an epoch that its Reset, Release and
// Drop end.
const genCheck = true

// A fixedRecord is the record of one allocation of a Fixed: its identity
// and its epoch registration.
type fixedRecord struct {
	id  uint64
	tok uintptr
}

func (r *fixedRecord) token() uintptr     { return r.tok }
func (r *fixedRecord) setToken(t uintptr) { r.tok = t }
