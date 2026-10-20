package escape

import "testing"

// TestReasonIsLazy checks that a reason builds its text once, when asked,
// and that a nil reason has none.
func TestReasonIsLazy(t *testing.T) {
	n := 0
	r := &reason{build: func() string { n++; return "stores p in G (a.p:1)" }}
	if n != 0 {
		t.Fatalf("built before it was needed")
	}
	first, second := r.String(), r.String()
	if first != "stores p in G (a.p:1)" || second != first || n != 1 {
		t.Fatalf("String built %d times", n)
	}
	var none *reason
	if none.String() != "" {
		t.Fatalf("nil reason has text %q", none.String())
	}
}

// TestSummaryKeyCaches checks that the sorted keys of a summary follow the
// additions made after they were first asked for.
func TestSummaryKeyCaches(t *testing.T) {
	var s summary
	if len(s.sortedEscapes()) != 0 || len(s.sortedStores()) != 0 {
		t.Fatalf("keys of an empty summary")
	}
	s.addEscape(pkey{idx: 2}, reason{text: "b"})
	first := s.sortedEscapes()
	s.addEscape(pkey{idx: 1}, reason{text: "a"})
	second := s.sortedEscapes()
	if len(first) != 1 || len(second) != 2 || second[0].idx != 1 || second[1].idx != 2 {
		t.Fatalf("escape keys %v then %v", first, second)
	}
	s.addStore(edge{src: pkey{idx: 3}, dst: pkey{idx: 0}}, &reason{text: "s"})
	s.addStore(edge{src: pkey{idx: 1}, dst: pkey{idx: 0}}, &reason{text: "s"})
	if st := s.sortedStores(); len(st) != 2 || st[0].src.idx != 1 {
		t.Fatalf("store keys %v", st)
	}
	s.addResult(0, pkey{idx: 5}, "")
	s.addResult(0, pkey{idx: 4}, "")
	if f := s.results[0].sortedFrom(); len(f) != 2 || f[0].idx != 4 {
		t.Fatalf("result keys %v", f)
	}
	s.addResult(0, pkey{idx: 3}, "")
	if f := s.results[0].sortedFrom(); len(f) != 3 || f[0].idx != 3 {
		t.Fatalf("result keys after an addition %v", f)
	}
}
