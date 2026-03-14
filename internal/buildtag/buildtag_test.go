package buildtag_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/buildtag"
)

func TestEvalSimple(t *testing.T) {
	tags := map[string]bool{"amd64": true, "linux": true}

	ok, err := buildtag.Eval("amd64", tags)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("amd64 should match")
	}

	ok, _ = buildtag.Eval("arm64", tags)
	if ok {
		t.Error("arm64 should not match")
	}
}

func TestEvalAnd(t *testing.T) {
	tags := map[string]bool{"amd64": true, "linux": true}
	ok, _ := buildtag.Eval("amd64 && linux", tags)
	if !ok {
		t.Error("amd64 && linux should match")
	}

	ok, _ = buildtag.Eval("amd64 && windows", tags)
	if ok {
		t.Error("amd64 && windows should not match")
	}
}

func TestEvalOr(t *testing.T) {
	tags := map[string]bool{"amd64": true}
	ok, _ := buildtag.Eval("amd64 || arm64", tags)
	if !ok {
		t.Error("amd64 || arm64 should match")
	}

	ok, _ = buildtag.Eval("arm64 || riscv64", tags)
	if ok {
		t.Error("arm64 || riscv64 should not match")
	}
}

func TestEvalNot(t *testing.T) {
	tags := map[string]bool{"amd64": true}
	ok, _ := buildtag.Eval("!freestanding", tags)
	if !ok {
		t.Error("!freestanding should match when not set")
	}

	ok, _ = buildtag.Eval("!amd64", tags)
	if ok {
		t.Error("!amd64 should not match when amd64 is set")
	}
}

func TestEvalComplex(t *testing.T) {
	tags := map[string]bool{"amd64": true, "linux": true}
	ok, _ := buildtag.Eval("(amd64 || arm64) && linux", tags)
	if !ok {
		t.Error("(amd64 || arm64) && linux should match")
	}

	ok, _ = buildtag.Eval("(amd64 || arm64) && windows", tags)
	if ok {
		t.Error("(amd64 || arm64) && windows should not match")
	}
}

func TestEvalIgnore(t *testing.T) {
	// The special tag "ignore" always makes the file excluded
	tags := map[string]bool{"amd64": true}
	ok, _ := buildtag.Eval("ignore", tags)
	if ok {
		t.Error("ignore tag should never match")
	}
}

func TestFileMatchesSuffix(t *testing.T) {
	tags := map[string]bool{"amd64": true, "linux": true}

	ok, _ := buildtag.FileMatches("foo_amd64.pu", nil, tags)
	if !ok {
		t.Error("foo_amd64.pu should match amd64")
	}

	ok, _ = buildtag.FileMatches("foo_arm64.pu", nil, tags)
	if ok {
		t.Error("foo_arm64.pu should not match on amd64")
	}

	ok, _ = buildtag.FileMatches("foo_amd64.s", nil, tags)
	if !ok {
		t.Error("foo_amd64.s should match amd64")
	}

	ok, _ = buildtag.FileMatches("foo.pu", nil, tags)
	if !ok {
		t.Error("foo.pu with no suffix should always match")
	}
}

func TestFileMatchesDirective(t *testing.T) {
	tags := map[string]bool{"amd64": true, "linux": true}

	ok, _ := buildtag.FileMatches("foo.pu", []string{"build amd64"}, tags)
	if !ok {
		t.Error("//pu:build amd64 should match")
	}

	ok, _ = buildtag.FileMatches("foo.pu", []string{"build arm64"}, tags)
	if ok {
		t.Error("//pu:build arm64 should not match on amd64")
	}

	ok, _ = buildtag.FileMatches("foo.pu", []string{"build amd64 && linux"}, tags)
	if !ok {
		t.Error("//pu:build amd64 && linux should match")
	}
}

func TestFileMatchesFreestanding(t *testing.T) {
	// freestanding mode excludes non-freestanding files
	hosted := map[string]bool{"amd64": true, "linux": true}
	freestanding := map[string]bool{"amd64": true, "freestanding": true}

	ok, _ := buildtag.FileMatches("syscall_linux.pu", []string{"build linux"}, hosted)
	if !ok {
		t.Error("syscall_linux should match in hosted mode")
	}

	ok, _ = buildtag.FileMatches("syscall_linux.pu", []string{"build linux"}, freestanding)
	if ok {
		t.Error("syscall_linux should not match in freestanding mode")
	}
}
