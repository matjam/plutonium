package hash

import (
	"pluto/io"
	"pluto/testing"
)

// The known FNV32a zero result must remain a valid initialized state.
func TestFNVZeroResult(t *testing.T) {
	data := [4]byte{204, 36, 49, 196}
	var h FNV32a
	h.Write(data[:])
	if h.Sum32() != 0 {
		t.Fatal("known zero result")
	}
	h.Write(nil)
	h.WriteString("")
	h.WriteByte(42)
	if h.Sum32() != 704659998 {
		t.Fatal("zero result lost across writes")
	}
}

func writeHello(t *testing.T, w io.Writer) {
	b := [5]byte{'h', 'e', 'l', 'l', 'o'}
	n, err := w.Write(b[:])
	if n != 5 || err != nil {
		t.Fatal("io.Writer contract")
	}
}

func TestStreamWriters(t *testing.T) {
	var a FNV32a
	var b FNV64a
	var key [16]byte
	h := NewSipHash24(key)
	writeHello(t, &a)
	writeHello(t, &b)
	writeHello(t, &h)
	if a.Sum32() != 0x4f9f2cab || b.Sum64() != 0xa430d84680aabd0b || h.Sum64() != 0x8cc15d5db2f752b9 {
		t.Fatal("hello hash")
	}
	c := h
	c.WriteByte('!')
	if c.Sum64() == h.Sum64() {
		t.Fatal("independent copy")
	}
	h.Reset()
	if h.Sum64() != 0x1e924b9d737700d7 {
		t.Fatal("reset preserves zero key")
	}
}
