package goversion

import (
	gover "go/version"
	"testing"
)

var versions = []string{
	"", "go", "go1", "go1.0", "go1.9", "go1.13", "go1.20", "go1.21", "go1.21rc1", "go1.21rc2", "go1.21.0",
	"go1.21.2", "go1.21.2rc1", "go1.22alpha1", "go1.22beta2", "go1.22", "go1.27", "go1.99999999999",
	"go1.23.4-custom", "go1.5-x", "1.21", "bad", "go01.2", "go1.02", "go1.2.03", "go1..2", "go1.2.", "go1.2rc",
	"go1.2rcx", "go1.2RC1", "go1.2.3.4", "go2", "go2.0", "go10.1", "go1.21-", "-go1.21", "go1.1e",
}

// TestAgainstGoVersion checks the fork against go/version.
func TestAgainstGoVersion(t *testing.T) {
	for _, x := range versions {
		if got, want := Lang(x), gover.Lang(x); got != want {
			t.Errorf("Lang(%q) = %q, go/version %q", x, got, want)
		}
		if got, want := IsValid(x), gover.IsValid(x); got != want {
			t.Errorf("IsValid(%q) = %v, go/version %v", x, got, want)
		}
		for _, y := range versions {
			if got, want := Compare(x, y), gover.Compare(x, y); got != want {
				t.Errorf("Compare(%q, %q) = %d, go/version %d", x, y, got, want)
			}
		}
	}
}
