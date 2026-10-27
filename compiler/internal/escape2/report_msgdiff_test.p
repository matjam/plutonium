// Ported from internal/escape2/report/msgdiff_test.go at the go-cutover tag.
// Differences from the Go test:
//   - It is in package escape2 (see report_helpers_test.p); firstLine is
//     escape2.p's, so the Go file's own copy is not repeated.
//   - testing.Short is PLUTO_LONG_TESTS: the test runs only when it is set.
//   - The corpus moved with the Go removal: cmd/pluto/testdata/diag is
//     tests/diag.
//   - Plutonium has no recover: a panic while writing the messages stops
//     the test instead of being reported as an error.
//   - The local site type is the package-level msgSite.

package escape2

import (
	"pluto/fmt"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/slices"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/diff"
	"compiler/internal/escape"
	"compiler/internal/escape2/report"
	"compiler/internal/escape2/sched"
	"compiler/internal/load"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

// TestMessageDiff compares, over the programs of the diagnostics goldens
// (cmd/pluto/testdata/diag), the escape messages of internal/escape with
// those this package writes for internal/escape2, as both mode would: every
// error one reports and the other does not, and every error both report at
// one line with one code whose full text (message, explanation, fixes)
// differs. The listing is logged (go test -v) and, when
// PLUTO_MSGDIFF names a file, written there.
//
// Differences are expected while the new analysis is completed (they are
// verdicts, which are not this package's); the test fails only on what is
// this package's: a message that names the analysis's own terms, or a
// program whose messages cannot be written.
func TestMessageDiff(t *testing.T) {
	t.Skip("knownfail: runs out of memory: nothing frees a golden program's load and analyses (Go's GC did), and the test passes 24 GB before it ends")
	if compat.Getenv("PLUTO_LONG_TESTS") == "" {
		t.Skip("loads every diagnostics golden program; set PLUTO_LONG_TESTS=1")
	}
	dirs := testenv.Glob(filepath.Join(testenv.Root(), "tests/diag/*"))
	if len(dirs) == 0 {
		t.Fatalf("no golden programs")
	}
	out := pu.Alloc[compat.Builder]()
	var st diffStats
	for _, dir := range dirs {
		if skipGolden(dir) {
			continue
		}
		cfg := pu.New(load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: testenv.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")})
		prog, err := load.Load(cfg, dir)
		if err != nil {
			continue // a golden of a load or type error
		}
		_, oldErr := escape.Analyze(prog)
		p, res, err := Run(prog, sched.Options{})
		if err != nil {
			fmt.Fprintf(out, "== %s\n  new analysis failed: %v\n", filepath.Base(dir), err)
			continue
		}
		newDiags := report.Diagnostics(prog, p, res)
		for _, d := range newDiags {
			if bad := internalTerm(render(d)); bad != "" {
				t.Errorf("%s: %s: message names %q:\n%s", filepath.Base(dir), d.Pos, bad, render(d))
			}
			if d.Msg == report.Unwritten {
				t.Errorf("%s: %s: the message could not be written", filepath.Base(dir), d.Pos)
			}
		}
		compareMessages(out, &st, filepath.Base(dir), escapeDiags(oldErr), newDiags)
	}
	fmt.Fprintf(out, "\nsummary: %d errors both report with the same text, %d with different text, %d old only, %d new only\n", st.same, st.differ, st.oldOnly, st.newOnly)
	t.Log("\n" + out.String())
	if name := compat.Getenv("PLUTO_MSGDIFF"); name != "" {
		if err := os.WriteFile(name, compat.Bytes(out.String()), 0o644); err != nil {
			t.Fatal(err.Error())
		}
	}
}

// skipGolden reports whether a golden program is built in a way the test
// does not reproduce (another subcommand, a kernel or freestanding mode).
func skipGolden(dir string) bool {
	for _, f := range []string{"flags", "command"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

type diffStats struct{ same, differ, oldOnly, newOnly int }

// escapeDiags returns the escape errors of an analysis's result: E3001 to
// E3009 and E3101, the codes both analyses report.
func escapeDiags(err error) []*diag.Diagnostic {
	var out []*diag.Diagnostic
	add := func(d *diag.Diagnostic) {
		if isEscapeCode(d.Code) {
			out = compat.Append(out, d)
		}
	}
	switch e := err.(type) {
	case *diag.List:
		for _, d := range *e {
			add(d)
		}
	case *diag.Diagnostic:
		add(e)
	}
	return out
}

func isEscapeCode(c diag.Code) bool {
	return c >= "E3001" && c <= "E3009" || c == diag.ClosureRegionLoop
}

// render is a diagnostic as pluto prints it, without the directory.
func render(d *diag.Diagnostic) string {
	dd := *d
	dd.Pos.Filename = filepath.Base(dd.Pos.Filename)
	return dd.Error() + "\n" + diag.Explanation(&dd)
}

// msgSite is where compareMessages matches errors: a line and a code.
type msgSite struct {
	line int
	code diag.Code
}

// compareMessages lists the differences between the old and new errors of
// one program. Errors are matched by line and code, as both mode matches
// sites; the column is part of the text compared.
func compareMessages(out *compat.Builder, st *diffStats, name string, old, new []*diag.Diagnostic) {
	key := func(d *diag.Diagnostic) msgSite { return msgSite{d.Pos.Line, d.Code} }
	oldBy, newBy := map[msgSite][]*diag.Diagnostic{}, map[msgSite][]*diag.Diagnostic{}
	var sites []msgSite
	for _, d := range old {
		k := key(d)
		if len(oldBy[k]) == 0 && len(newBy[k]) == 0 {
			sites = compat.Append(sites, k)
		}
		oldBy[k] = compat.Append(oldBy[k], d)
	}
	for _, d := range new {
		k := key(d)
		if len(oldBy[k]) == 0 && len(newBy[k]) == 0 {
			sites = compat.Append(sites, k)
		}
		newBy[k] = compat.Append(newBy[k], d)
	}
	slices.SortFunc(sites, func(a, b msgSite) int {
		if a.line != b.line {
			return a.line - b.line
		}
		if a.code < b.code {
			return -1
		}
		if a.code > b.code {
			return 1
		}
		return 0
	})
	b := pu.Alloc[compat.Builder]()
	for _, k := range sites {
		os, ns := pairUp(oldBy[k], newBy[k])
		for i := range os {
			switch {
			case os[i] != nil && ns[i] == nil:
				st.oldOnly++
				fmt.Fprintf(b, "  old only: %s\n", firstLine(render(os[i])))
			case os[i] == nil && ns[i] != nil:
				st.newOnly++
				fmt.Fprintf(b, "  new only: %s\n", firstLine(render(ns[i])))
			default:
				ot, nt := render(os[i]), render(ns[i])
				if ot == nt {
					st.same++
					continue
				}
				st.differ++
				fmt.Fprintf(b, "  text differs at line %d %s:\n", k.line, k.code)
				for _, l := range compat.Split(strings.TrimRight(compat.String(diff.Diff("old", compat.Bytes(ot), "new", compat.Bytes(nt))), "\n"), "\n") {
					fmt.Fprintf(b, "    %s\n", l)
				}
			}
		}
	}
	if b.Len() > 0 {
		fmt.Fprintf(out, "== %s\n%s", name, b.String())
	}
}

// pairUp matches the errors of one site: those with the same message line
// first, then the rest in order. Either result may hold nil where one
// analysis has no partner.
func pairUp(old, new []*diag.Diagnostic) (os, ns []*diag.Diagnostic) {
	usedN := compat.Make[bool](len(new))
	usedO := compat.Make[bool](len(old))
	for i, o := range old {
		for j, n := range new {
			if !usedN[j] && o.Msg == n.Msg {
				usedO[i], usedN[j] = true, true
				os, ns = compat.Append(os, o), compat.Append(ns, n)
				break
			}
		}
	}
	var restO, restN []*diag.Diagnostic
	for i, o := range old {
		if !usedO[i] {
			restO = compat.Append(restO, o)
		}
	}
	for j, n := range new {
		if !usedN[j] {
			restN = compat.Append(restN, n)
		}
	}
	for len(restO) > 0 || len(restN) > 0 {
		var o, n *diag.Diagnostic
		if len(restO) > 0 {
			o, restO = restO[0], restO[1:]
		}
		if len(restN) > 0 {
			n, restN = restN[0], restN[1:]
		}
		os, ns = compat.Append(os, o), compat.Append(ns, n)
	}
	return os, ns
}

// internalTerms are words of the analysis's implementation that a message
// must not use (AGENTS.md, Diagnostics): the program's terms only.
var internalTerms = []string{"register", "token", "summary", "EIR", "eir.", "%!", "Obj(", "Reg(", "In(", "SCC", "witness", "<nil>"}

// internalTerm returns the first implementation term or absolute path in
// text, or "".
func internalTerm(text string) string {
	for _, w := range internalTerms {
		if strings.Contains(text, w) {
			return w
		}
	}
	for _, f := range compat.Fields(text) {
		if strings.HasPrefix(strings.TrimLeft(f, "("), "/") && strings.Contains(f, ".p:") {
			return f
		}
	}
	return ""
}
