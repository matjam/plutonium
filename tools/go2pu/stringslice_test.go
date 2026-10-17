package main

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestStringSliceRewrite translates the testdata/rewrites program and checks
// that each form of slice expression on a string becomes a call of
// pluto/strings (language spec section 11.12), and that the slice of a byte
// slice stays a slice expression. TestDifferential builds and runs the
// translation.
func TestStringSliceRewrite(t *testing.T) {
	testguard.Parallel(t)
	res, err := TranslateDir(testConfig(), "testdata/rewrites")
	if err != nil {
		t.Fatal(err)
	}
	var src string
	for _, f := range res.Files {
		if strings.HasSuffix(f.Name, "main.go") || strings.HasSuffix(f.Name, "main.p") {
			src = string(f.Src)
		}
	}
	if src == "" {
		t.Fatal("no translation of main.go")
	}
	for _, want := range []string{
		`"pluto/strings"`,
		"strings.Slice(t, 1, n)",  // t[1:n]
		"strings.SliceFrom(t, n)", // t[n:]
		"strings.Slice(t, 0, n)",  // t[:n]
		"compat.String(bs0(t)[1:3])",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("translation lacks %q", want)
		}
	}
	if strings.Contains(src, "t[1:n]") || strings.Contains(src, "t[n:]") || strings.Contains(src, "t[:n]") || strings.Contains(src, "t[:]") {
		t.Error("translation still slices a string with a slice expression")
	}
}
