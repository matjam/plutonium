// Translated by go2pu from internal/escape2/solve/heapbytes.go at the go-cutover tag; edited by hand since.

package solve

import (
	"selfhost/internal/escape2/summary"
)

// heapBytes estimates what the solver holds outside its arena and set
// table: the Go slices and maps of its nodes, applications and calls of
// function values. A kept state (State) holds them as long as it is kept,
// so State.Bytes counts them: the scheduler keeps states under a budget of
// bytes (sched/incr.go, sched/parallel.go), which must see all of a state.
// The sizes are estimates of the Go runtime's: a slice element by its
// type's size, a map entry by its key and value and a fixed overhead.
func (s *solver) heapBytes() int {
	const (
		ptr      = 8
		sliceHdr = 24
		mapEntry = 48 // a map entry's overhead beside its key and value
	)
	n := (cap(s.lists) + cap(s.bits)) * sliceHdr
	n += cap(s.assertT) * 16
	n += cap(s.stepBuf) * 4
	n += cap(s.applied) * 56
	n += cap(s.findings) * 56
	n += cap(s.argBuf)
	n += cap(s.dcalls) * ptr
	for _, d := range s.dcalls {
		n += 96 + cap(d.args)*4 + cap(d.keys)*8
	}
	n += cap(s.fcalls) * ptr
	n += len(s.fcalls) * 40
	n += len(s.closureOf) * (mapEntry + 8)
	for k := range s.dcallIdx {
		n += mapEntry + len(k) + 24
	}
	n += len(s.dcallDone) * (mapEntry + 17)
	for _, gs := range s.deferOrigins {
		n += mapEntry + 32 + cap(gs)*8
	}
	for _, ds := range s.originDcalls {
		n += mapEntry + 32 + cap(ds)*8
	}
	for _, as := range s.closApps {
		n += mapEntry + 40 + cap(as)*4
	}
	for _, g := range s.fbGroups {
		n += mapEntry + 24 + 40 + cap(g.args)*4
	}
	for k := range s.argNodes {
		n += mapEntry + len(k) + 20
	}
	// The summaries the calls applied: a kept state holds the version it
	// applied of each (Resume applies what they gained since), so it keeps
	// it alive after the scheduler has replaced it. Counted for each state
	// that holds it.
	seen := map[*summary.Summary]bool{}
	for _, ap := range s.applied {
		if ap.sum != nil && !seen[ap.sum] {
			seen[ap.sum] = true
			n += summaryBytes(ap.sum)
		}
	}
	return n
}

// summaryBytes estimates the memory of a summary.
func summaryBytes(sum *summary.Summary) int {
	n := 160 + 8*len(sum.Esc) + 16*len(sum.Store) + 8*len(sum.HeapStore) + 8*len(sum.Region.OuterResults) + 16*len(sum.Reasons)
	for _, r := range sum.Res {
		n += 72 + 24*len(r.From) + 4*len(r.Heap) + 4*len(r.CountedHeap)
	}
	for _, d := range sum.Deferred {
		n += 48 + 32*len(d.Args)
		for _, a := range d.Args {
			n += 8 * len(a.Keys)
		}
	}
	return n
}
