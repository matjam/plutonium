package lower_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

// TestItabTable lowers a program with many itabs and checks the itab table
// the runtime searches (pluto.itabs): a power-of-two number of slots, at
// most half full, in which a lookup that starts where pluto/rt's findItab
// starts and probes forward finds every itab before an empty slot.
func TestItabTable(t *testing.T) {
	var src strings.Builder
	src.WriteString("package main\n\n")
	const nTypes, nIfaces = 40, 6
	for i := 0; i < nIfaces; i++ {
		fmt.Fprintf(&src, "type I%d interface{ M%d() int }\n", i, i)
	}
	for i := 0; i < nTypes; i++ {
		fmt.Fprintf(&src, "type T%d struct{ p *int }\n", i)
		for j := 0; j < nIfaces; j++ {
			if (i+j)%3 != 0 {
				fmt.Fprintf(&src, "func (T%d) M%d() int { return %d }\n", i, j, 100*i+j)
			}
		}
	}
	fmt.Fprintf(&src, "var sink [%d]any\n\nfunc main() {\n", nTypes)
	for i := 0; i < nTypes; i++ {
		fmt.Fprintf(&src, "\tsink[%d] = T%d{}\n", i, i)
	}
	src.WriteString("\tfor _, v := range sink {\n")
	for j := 0; j < nIfaces; j++ {
		fmt.Fprintf(&src, "\t\tif x, ok := v.(I%d); ok {\n\t\t\tprintln(x.M%d())\n\t\t}\n", j, j)
	}
	src.WriteString("\t}\n}\n")
	prog := lowerSource(t, src.String())

	data := map[string]*ssa.Data{}
	for _, d := range prog.Data {
		data[d.Sym.Name] = d
	}
	table := data["pluto.itabs"]
	if table == nil {
		t.Fatal("no pluto.itabs")
	}
	size := int(binary.LittleEndian.Uint64(table.Bytes))
	if size&(size-1) != 0 {
		t.Fatalf("table size %d is not a power of two", size)
	}
	slots := make([]*ssa.Sym, size)
	for _, r := range table.Relocs {
		slots[r.Off/8-1] = r.Sym
	}
	descHash := func(s *ssa.Sym) uint32 { return binary.LittleEndian.Uint32(data[s.Name].Bytes[8:]) }
	n := 0
	for _, d := range prog.Data {
		if !strings.HasPrefix(d.Sym.Name, "itab:") {
			continue
		}
		n++
		var typ, iface *ssa.Sym
		for _, r := range d.Relocs {
			switch r.Off {
			case 0:
				typ = r.Sym
			case 8:
				iface = r.Sym
			}
		}
		h := descHash(typ)*0x9E3779B1 ^ descHash(iface)
		h ^= h >> 15
		h *= 0x85EBCA6B
		h ^= h >> 13
		found := false
		for i := int(h) & (size - 1); slots[i] != nil; i = (i + 1) & (size - 1) {
			if slots[i] == d.Sym {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is not found by a lookup", d.Sym.Name)
		}
	}
	if want := nTypes * nIfaces * 2 / 3; n < want {
		t.Errorf("%d itabs, want at least %d", n, want)
	}
	if 2*n > size {
		t.Errorf("%d itabs in %d slots: more than half full", n, size)
	}
}
