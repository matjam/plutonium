package report_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/diff"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/escape2/report"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
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
	if testing.Short() {
		t.Skip("loads every diagnostics golden program")
	}
	dirs, err := filepath.Glob("../../../cmd/pluto/testdata/diag/*")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no golden programs: %v", err)
	}
	var out strings.Builder
	var st diffStats
	for _, dir := range dirs {
		if skipGolden(dir) {
			continue
		}
		cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
		prog, err := load.Load(cfg, dir)
		if err != nil {
			continue // a golden of a load or type error
		}
		_, oldErr := escape.Analyze(prog)
		p, res, err := escape2.Run(prog, sched.Options{})
		if err != nil {
			fmt.Fprintf(&out, "== %s\n  new analysis failed: %v\n", filepath.Base(dir), err)
			continue
		}
		var newDiags []*diag.Diagnostic
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: writing the messages panicked: %v", filepath.Base(dir), r)
				}
			}()
			newDiags = report.Diagnostics(prog, p, res)
		}()
		for _, d := range newDiags {
			if bad := internalTerm(render(d)); bad != "" {
				t.Errorf("%s: %s: message names %q:\n%s", filepath.Base(dir), d.Pos, bad, render(d))
			}
			if d.Msg == report.Unwritten {
				t.Errorf("%s: %s: the message could not be written", filepath.Base(dir), d.Pos)
			}
		}
		compareMessages(&out, &st, filepath.Base(dir), escapeDiags(oldErr), newDiags)
	}
	fmt.Fprintf(&out, "\nsummary: %d errors both report with the same text, %d with different text, %d old only, %d new only\n", st.same, st.differ, st.oldOnly, st.newOnly)
	t.Log("\n" + out.String())
	if name := os.Getenv("PLUTO_MSGDIFF"); name != "" {
		if err := os.WriteFile(name, []byte(out.String()), 0o644); err != nil {
			t.Fatal(err)
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
			out = append(out, d)
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

// compareMessages lists the differences between the old and new errors of
// one program. Errors are matched by line and code, as both mode matches
// sites; the column is part of the text compared.
func compareMessages(out *strings.Builder, st *diffStats, name string, old, new []*diag.Diagnostic) {
	type site struct {
		line int
		code diag.Code
	}
	key := func(d *diag.Diagnostic) site { return site{d.Pos.Line, d.Code} }
	oldBy, newBy := map[site][]*diag.Diagnostic{}, map[site][]*diag.Diagnostic{}
	var sites []site
	for _, d := range old {
		k := key(d)
		if len(oldBy[k]) == 0 && len(newBy[k]) == 0 {
			sites = append(sites, k)
		}
		oldBy[k] = append(oldBy[k], d)
	}
	for _, d := range new {
		k := key(d)
		if len(oldBy[k]) == 0 && len(newBy[k]) == 0 {
			sites = append(sites, k)
		}
		newBy[k] = append(newBy[k], d)
	}
	sort.Slice(sites, func(i, j int) bool {
		if sites[i].line != sites[j].line {
			return sites[i].line < sites[j].line
		}
		return sites[i].code < sites[j].code
	})
	var b strings.Builder
	for _, k := range sites {
		os, ns := pairUp(oldBy[k], newBy[k])
		for i := range os {
			switch {
			case os[i] != nil && ns[i] == nil:
				st.oldOnly++
				fmt.Fprintf(&b, "  old only: %s\n", firstLine(render(os[i])))
			case os[i] == nil && ns[i] != nil:
				st.newOnly++
				fmt.Fprintf(&b, "  new only: %s\n", firstLine(render(ns[i])))
			default:
				ot, nt := render(os[i]), render(ns[i])
				if ot == nt {
					st.same++
					continue
				}
				st.differ++
				fmt.Fprintf(&b, "  text differs at line %d %s:\n", k.line, k.code)
				for _, l := range strings.Split(strings.TrimRight(string(diff.Diff("old", []byte(ot), "new", []byte(nt))), "\n"), "\n") {
					fmt.Fprintf(&b, "    %s\n", l)
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
	usedN := make([]bool, len(new))
	usedO := make([]bool, len(old))
	for i, o := range old {
		for j, n := range new {
			if !usedN[j] && o.Msg == n.Msg {
				usedO[i], usedN[j] = true, true
				os, ns = append(os, o), append(ns, n)
				break
			}
		}
	}
	var restO, restN []*diag.Diagnostic
	for i, o := range old {
		if !usedO[i] {
			restO = append(restO, o)
		}
	}
	for j, n := range new {
		if !usedN[j] {
			restN = append(restN, n)
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
		os, ns = append(os, o), append(ns, n)
	}
	return os, ns
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
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
	for _, f := range strings.Fields(text) {
		if strings.HasPrefix(strings.TrimLeft(f, "("), "/") && strings.Contains(f, ".p:") {
			return f
		}
	}
	return ""
}
