// Ported from internal/build/run_test.go at the go-cutover tag.

package tests

import (
	"testing"
)

func TestReturnStatus(t *testing.T) {
	t.Parallel()
	_, status := runSrc(t, "package main\n\nfunc main() int64 { return 42 }\n", 0)
	if status != 42 {
		t.Fatalf("exit status %d, want 42", status)
	}
}
