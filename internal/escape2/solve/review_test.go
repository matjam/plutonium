package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// The programs of the adversarial reviews of the escape analysis: each of
// review2, review3 (internal/escape/testdata) and review4 (testdata/review4,
// the fourth review's programs) keeps, returns or stores a pointer into a
// frame that is gone, and must be rejected by this analysis; the controls
// (want.txt "accepted") must be accepted. The fourth review's programs have
// no controls.
//
// A program this analysis cannot reject yet for a reason outside the solver
// (a gap of a stage not built, named in knownGaps) is listed there with the
// reason; the test fails when such a program starts being rejected, so that
// the list stays true.

// knownGaps are the programs not rejected yet, with the reason.
// untriaged marks a program not yet looked at: the two examined of each
// kind were gaps of the EIR builder (named in the entries below), not of
// the solver.
const untriaged = "untriaged: EIR builder, or a later stage (3: tags; 4: loops, owners, arenas)"

var knownGaps = map[string]string{
	"review2/f38_fmt_named_slice_type_string":             untriaged,
	"review3/o14_drop_in_vec_of_owners_local":             untriaged,
	"review4/e08_errorbuf_sprint_control":                 untriaged,
	"review4/s21_fromutf8_local_bytes":                    untriaged,
	"review4/s22_fromutf8lossy_local_bytes":               untriaged,
	"review4/s23_builder_string_kept":                     untriaged,
}

// knownFalseRejections are controls this analysis rejects, with the reason.
var knownFalseRejections = map[string]string{}

func loadFile(t *testing.T, path string) *load.Program {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Skipf("load: %v", err) // the type checker rejects it: not this analysis's case
	}
	return prog
}

// controls reads the names a want.txt marks "accepted".
func controls(t *testing.T, dir string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(dir, "want.txt"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, first, ok := strings.Cut(line, ": ")
		if ok && first == "accepted" {
			out[name] = true
		}
	}
	return out
}

func TestReviews(t *testing.T) {
	if testing.Short() {
		t.Skip("loads every review program")
	}
	for _, dir := range []string{
		"../../escape/testdata/review2",
		"../../escape/testdata/review3",
		"testdata/review4",
	} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.p"))
		if len(files) == 0 {
			t.Fatalf("no programs in %s", dir)
		}
		ctl := controls(t, dir)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			for _, f := range files {
				name := strings.TrimSuffix(filepath.Base(f), ".p")
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					prog := loadFile(t, f)
					err := escape2.Check(prog)
					if err != nil && strings.Contains(err.Error(), "internal compiler error") {
						t.Fatalf("internal error: %v", err)
					}
					// Only errors in the program itself count: the library's are
					// false rejections of their own (listed by TestLibrary).
					if err != nil && !strings.Contains(err.Error(), "/main.p:") {
						err = nil
					}
					gap, isGap := knownGaps[filepath.Base(dir)+"/"+name]
					switch {
					case ctl[name] && err != nil && knownFalseRejections[filepath.Base(dir)+"/"+name] == "":
						t.Errorf("control rejected: %v", err)
					case ctl[name] && err == nil && knownFalseRejections[filepath.Base(dir)+"/"+name] != "":
						t.Errorf("control accepted now; remove it from knownFalseRejections")
					case !ctl[name] && err == nil && !isGap:
						t.Errorf("accepted: the program keeps a pointer into a dead frame")
					case !ctl[name] && err != nil && isGap:
						t.Errorf("rejected now; remove it from knownGaps (%s)", gap)
					}
				})
			}
		})
	}
}

// main.(*Tree).Insert of testdata/run/gentypes.p, recursive through its
// left and right subtrees, did not converge in its SCC: its store pairs grew
// past the load limit, were cut back, and the solve against the cut summary
// lost what the cut covered, so the summary went back and forth. The
// scheduler now joins each new summary with the previous one (summary.Join).
func TestTreeInsertConverges(t *testing.T) {
	prog := loadFile(t, "../../../testdata/run/gentypes.p")
	if err := escape2.Check(prog); err != nil && strings.Contains(err.Error(), "internal compiler error") {
		t.Fatal(err)
	}
}
