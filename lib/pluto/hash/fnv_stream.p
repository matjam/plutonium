package hash

const (
	fnvOffset32 = 2166136261
	fnvPrime32  = 16777619
)

// FNV32a is an allocation-free 32-bit FNV-1a stream. Its zero value is
// ready to use. Assignment copies the stream independently. FNV is not
// suitable for defending a hash table against attacker-chosen keys.
type FNV32a struct {
	sum         uint32
	initialized bool
}

// Reset discards the bytes written so far.
func (h *FNV32a) Reset() { h.sum, h.initialized = fnvOffset32, true }

// Sum32 returns the hash without changing the stream.
func (h FNV32a) Sum32() uint32 {
	if !h.initialized {
		return fnvOffset32
	}
	return h.sum
}

// Write consumes all bytes of b and returns len(b), nil. It retains no view.
func (h *FNV32a) Write(b []byte) (int, error) {
	s := h.Sum32()
	for _, c := range b {
		s = (s ^ uint32(c)) * fnvPrime32
	}
	h.sum, h.initialized = s, true
	return len(b), nil
}

// WriteString consumes all bytes of s and returns len(s), nil.
func (h *FNV32a) WriteString(s string) (int, error) {
	v := h.Sum32()
	for i := 0; i < len(s); i++ {
		v = (v ^ uint32(s[i])) * fnvPrime32
	}
	h.sum, h.initialized = v, true
	return len(s), nil
}

// WriteByte writes c and returns nil.
func (h *FNV32a) WriteByte(c byte) error {
	h.sum = (h.Sum32() ^ uint32(c)) * fnvPrime32
	h.initialized = true
	return nil
}

// FNV64a is an allocation-free 64-bit FNV-1a stream. Its zero value is
// ready to use. Assignment copies the stream independently. FNV is not
// suitable for defending a hash table against attacker-chosen keys.
type FNV64a struct {
	sum         uint64
	initialized bool
}

// Reset discards the bytes written so far.
func (h *FNV64a) Reset() { h.sum, h.initialized = fnvOffset64, true }

// Sum64 returns the hash without changing the stream.
func (h FNV64a) Sum64() uint64 {
	if !h.initialized {
		return fnvOffset64
	}
	return h.sum
}

// Write consumes all bytes of b and returns len(b), nil. It retains no view.
func (h *FNV64a) Write(b []byte) (int, error) {
	s := h.Sum64()
	for _, c := range b {
		s = (s ^ uint64(c)) * fnvPrime64
	}
	h.sum, h.initialized = s, true
	return len(b), nil
}

// WriteString consumes all bytes of s and returns len(s), nil.
func (h *FNV64a) WriteString(s string) (int, error) {
	v := h.Sum64()
	for i := 0; i < len(s); i++ {
		v = (v ^ uint64(s[i])) * fnvPrime64
	}
	h.sum, h.initialized = v, true
	return len(s), nil
}

// WriteByte writes c and returns nil.
func (h *FNV64a) WriteByte(c byte) error {
	h.sum = (h.Sum64() ^ uint64(c)) * fnvPrime64
	h.initialized = true
	return nil
}
