//pu:build gencheck

package scratch

import (
	"pluto/testing"
	"unsafe"

	"compiler/internal/testpanic"
)

// With generation checks a pointer kept past the end of its memory panics
// where it is used (language spec section 18.11). Without them the read
// below is a read of freed memory, which nothing catches, so these tests
// are for checked builds only.

func TestRegionUseAfterRelease(t *testing.T) {
	r := NewRegion()
	u := r.Use()
	n := fill(100)
	u.Restore()
	if testpanic.Panics(func() { _ = sum(n) }) {
		t.Fatal("reading a live region's node panics")
	}
	r.Release()
	if !testpanic.Panics(func() { _ = sum(n) }) {
		t.Error("reading a node of a released region does not panic")
	}
}

func TestHomeUseAfterDrop(t *testing.T) {
	key := unsafe.Pointer(&homeKeys[0])
	BeginHome(key)
	n := fill(100)
	EndHome()
	if testpanic.Panics(func() { _ = sum(n) }) {
		t.Fatal("reading a live home's node panics")
	}
	Drop(key)
	if !testpanic.Panics(func() { _ = sum(n) }) {
		t.Error("reading a node of a dropped home does not panic")
	}
}

func TestHomeUseAfterReplace(t *testing.T) {
	key := unsafe.Pointer(&homeKeys[1])
	var s keepStep
	DoHome(key, &s)
	old := s.kept
	DoHome(key, &s)
	if testpanic.Panics(func() { _ = sum(s.kept) }) {
		t.Fatal("reading the newest kept result panics")
	}
	if !testpanic.Panics(func() { _ = sum(old) }) {
		t.Error("reading a replaced kept result does not panic")
	}
	Drop(key)
}

func TestDoUseAfterStep(t *testing.T) {
	var s keepStep
	Do(&s)
	if !testpanic.Panics(func() { _ = sum(s.work) }) {
		t.Error("reading a finished step's scratch memory does not panic")
	}
}
