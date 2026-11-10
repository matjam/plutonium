//pu:build !gencheck

package arena

// genCheck is off: arena pointers carry no epoch (epoch.p).
const genCheck = false

// A fixedRecord is the record of one allocation of a Fixed: its identity
// and its offset in the payload. Without epochs it has no registration.
type fixedRecord struct {
	id    uint64
	start uintptr
}

func (r *fixedRecord) token() uintptr     { return 0 }
func (r *fixedRecord) setToken(t uintptr) {}
