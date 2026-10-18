package lsp

import (
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/directive"
)

// TestHoverDirectives is part of the directive audit (spec section 20.3): the
// hover text covers exactly the directives the compiler accepts, and the form
// //pu:interrupt trap has a text of its own.
func TestHoverDirectives(t *testing.T) {
	var got []string
	for n := range directives {
		got = append(got, n)
	}
	slices.Sort(got)
	if want := directive.Names(); !slices.Equal(got, want) {
		t.Errorf("hover text for %v, the compiler accepts %v", got, want)
	}
	plain, trap := directiveMeaning("//pu:interrupt"), directiveMeaning("//pu:interrupt trap")
	if plain == "" || trap == "" || plain == trap {
		t.Errorf("the hover texts of //pu:interrupt and //pu:interrupt trap: %q and %q", plain, trap)
	}
	for _, c := range []string{"interrupt gate", "x86.Sti", "//pu:interrupt trap"} {
		if !strings.Contains(plain, c) {
			t.Errorf("the hover text of //pu:interrupt does not mention %s: %q", c, plain)
		}
	}
	for _, c := range []string{"trap gate", "not a region with interrupts disabled"} {
		if !strings.Contains(trap, c) {
			t.Errorf("the hover text of //pu:interrupt trap does not mention %s: %q", c, trap)
		}
	}
	if directiveMeaning("//pu:frobnicate") != "" {
		t.Error("an unknown directive has a hover text")
	}
}
