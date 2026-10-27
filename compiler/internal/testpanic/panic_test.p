package testpanic

import "pluto/testing"

func TestPanics(t *testing.T) {
	var s []int
	if !Panics(func() { panic("boom") }) {
		t.Error("a panic was not seen")
	}
	if !Panics(func() { _ = s[3] }) {
		t.Error("an index out of range was not seen")
	}
	if Panics(func() {}) {
		t.Error("a return was taken for a panic")
	}
}
